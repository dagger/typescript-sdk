package typescriptgenerator

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

var updateFixtures = flag.Bool("test.update-fixtures", false, "update the test fixtures")

// TestGenerateEntrypoint renders the static dispatch entrypoint from a typedef
// captured by running the real SDK introspector over a module exercising the
// shapes dispatch has to handle: a constructor with a defaulted argument,
// exposed fields (including an object one, which round-trips through an ID), an
// optional argument, an async method, and a void return.
//
// The fixture is the introspector's own output rather than hand-written JSON,
// so this pins the renderer against the contract it actually receives.
func TestGenerateEntrypoint(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
		},
	}}

	state, err := gen.GenerateEntrypoint(context.Background())
	require.NoError(t, err)

	got := readOverlay(t, state, DefaultEntrypointFile)

	const goldenPath = "testdata/entrypoint_smoke_want.ts"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	require.Equal(t, string(want), got)
}

// TestGenerateEntrypointIfaceReturns pins how an interface method wraps an
// object-ish result. Every case has to reach a different wrapper — the same
// interface, a sibling interface, a module object, a core object — and the
// renderer wrapped all of them as the declaring interface, so calling a method
// on a returned Container reached a Greeter's dispatch instead.
//
// The list branch three lines below the bug already dispatched correctly, which
// is what makes the singular one clearly a slip rather than a design.
func TestGenerateEntrypointIfaceReturns(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_iface_returns.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
		},
	}}

	state, err := gen.GenerateEntrypoint(context.Background())
	require.NoError(t, err)

	got := readOverlay(t, state, DefaultEntrypointFile)

	const goldenPath = "testdata/entrypoint_iface_returns_want.ts"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	require.Equal(t, string(want), got)
}

// TestGenerateEntrypointDispatch pins the manifest-v2 dispatcher, generated
// beside the legacy entrypoint rather than in place of it.
//
// It is the same file minus register(): under a module entrypoint the typedefs
// come from the generated Dang program, so the container only ever runs a real
// call. What that leaves is the dispatch half behind a different protocol — one
// JSON request on stdin, one JSON result on stdout.
func TestGenerateEntrypointDispatch(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    true,
		},
	}}

	state, err := gen.GenerateEntrypoint(context.Background())
	require.NoError(t, err)

	got := readOverlay(t, state, DefaultDispatchFile)

	const goldenPath = "testdata/dispatch_smoke_want.ts"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	require.Equal(t, string(want), got)
}

// TestGenerateEntrypointDispatchDropsRegister pins what the protocol change
// actually removes, and the one thing it is easy to get wrong.
//
// register() and the engine's FunctionCall channel are gone, so the typedef
// imports that only they used go too. The subtle part is LogOutput: stdout now
// carries the result, so the session's logs have to move to stderr or they
// corrupt it.
func TestGenerateEntrypointDispatchDropsRegister(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    true,
		},
	}}

	state, err := gen.GenerateEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDispatchFile)

	for _, gone := range []string{
		"function register(",
		"currentFunctionCall",
		"returnValue",
		"returnError",
		"TypeDefKind",
		"FunctionCachePolicy",
		"DaggerError",
	} {
		require.NotContains(t, got, gone)
	}

	require.Contains(t, got, "{ LogOutput: process.stderr }")
	require.NotContains(t, got, "process.stdout }")

	// The dispatch half is untouched: same invoke(), same state helpers.
	require.Contains(t, got, "async function invoke(")
	require.Contains(t, got, "function rebuildSmoke(")
	require.Contains(t, got, "async function serializeSmoke(")

	// One mode, and only one. A hand-invoked mode was tried and dropped: a
	// module's own client cannot obtain a Workspace — the module-facing schema
	// has no currentWorkspace and no host access — so the default template's
	// `constructor(ws: Workspace)` could only ever have been called with an
	// undefined receiver.
	require.Contains(t, got, `mode !== "engine-call"`)
	require.NotContains(t, got, "developerCall")
}

// TestGenerateEntrypoint_RequiresTypedef guards the one input the renderer
// cannot do without: unlike the binding generators it never sees the schema, so
// a missing typedef leaves it with nothing to dispatch.
func TestGenerateEntrypoint_RequiresTypedef(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{},
	}}

	_, err := gen.GenerateEntrypoint(context.Background())
	require.ErrorContains(t, err, "TypedefJSONPath is required")
}

// TestGenerateEntrypointDispatchDoesNotServe pins that the dispatcher no longer
// serves anything. Each generated client serves its own module before the first
// query that reaches it — a `dag.<module>()` call through the client's served
// context, or a received value loaded through the loader's served context — so
// the dispatcher does not, and never imports dag for it.
func TestGenerateEntrypointDispatchDoesNotServe(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    true,
		},
	}}

	state, err := gen.GenerateEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDispatchFile)

	require.NotContains(t, got, "serveBoundModules")
	require.NotContains(t, got, "currentWorkspace")
	require.NotContains(t, got, ".asModule().serve()")
	require.Contains(t, got, `import { Context, connection, getRegisteredClass }`)
	// The loader is still imported for wrapping received IDs; it is what serves.
	require.Contains(t, got, "__loadObject as __loadCoreObject")
}
