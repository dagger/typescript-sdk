package typescriptgenerator

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

// TestGenerateDangEntrypoint pins the Dang program against the same typedef the
// dispatcher is rendered from, so the two halves of a module entrypoint stay
// derived from one scan. The fixture is the introspector's own output, and it
// covers what has to survive the retarget from TypeScript to Dang: a defaulted
// constructor argument, an exposed object field, an optional argument, a list
// argument, an enum with renamed members, an interface, and a void return.
func TestGenerateDangEntrypoint(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
			ModulePath:      ".",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)

	got := readOverlay(t, state, DefaultDangEntrypointFile)

	const goldenPath = "testdata/dang_entrypoint_smoke_want.dang"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	require.Equal(t, string(want), got)
}

// TestGenerateDangEntrypointRuntimes pins the one part of the program that is not
// derived from the typedef: the container recipe call() bakes. Each runtime needs
// a different image, a different way to reach the dispatcher, and — for deno,
// which resolves @dagger.io/dagger through deno.json rather than node_modules —
// a different mount layout.
func TestGenerateDangEntrypointRuntimes(t *testing.T) {
	for _, tc := range []struct {
		runtime  string
		image    string
		exec     string
		wantsSDK bool
	}{
		{
			runtime:  "node",
			image:    "node:24.13.1-alpine@sha256:",
			exec:     `["tsx", "--no-deprecation", "--tsconfig", "tsconfig.json", "__dagger.dispatch.ts", "engine-call"]`,
			wantsSDK: true,
		},
		{
			runtime:  "bun",
			image:    "oven/bun:1.3.0-alpine@sha256:",
			exec:     `["bun", "run", "__dagger.dispatch.ts", "engine-call"]`,
			wantsSDK: true,
		},
		{
			runtime:  "deno",
			image:    "denoland/deno:alpine-2.5.0@sha256:",
			exec:     `["deno", "run", "-q", "-A", "__dagger.dispatch.ts", "engine-call"]`,
			wantsSDK: false,
		},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					Runtime:         tc.runtime,
					ModulePath:      ".",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			require.Contains(t, got, tc.image)
			require.Contains(t, got, tc.exec)

			const sdkMount = `withMountedDirectory("node_modules/@dagger.io/dagger"`
			if tc.wantsSDK {
				require.Contains(t, got, sdkMount)
			} else {
				require.NotContains(t, got, sdkMount)
			}

			// tsx is a node-only loader, installed because a generated entrypoint
			// has no engine image to mount it out of.
			if tc.runtime == "node" {
				require.Contains(t, got, `"npm", "install", "-g", "tsx@`)
			} else {
				require.NotContains(t, got, "tsx@")
			}
		})
	}
}

// TestGenerateDangEntrypointNestedModule covers a module that is not the
// workspace root. call() receives the *caller's* workspace, so the module's own
// path has to be baked at generation rather than read from workspace.cwd —
// otherwise a call from a subdirectory runs the dispatcher in the wrong place.
func TestGenerateDangEntrypointNestedModule(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
			ModulePath:      "./.dagger/modules/smoke",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Contains(t, got, `withWorkdir("/workspace/.dagger/modules/smoke")`)
	require.Contains(t, got,
		`withMountedDirectory("node_modules/@dagger.io/dagger", workspace.directory("/.dagger/modules/smoke/sdk"))`)
}

// TestGenerateDangEntrypointSharedClients covers the shared workspace layout:
// the module keeps no sdk/ of its own, so the recipe mounts the whole shared
// client directory at node_modules/@dagger.io — its subdirectories map 1:1
// onto the @dagger.io package names — and the generated-file guard checks the
// library there and the loader beside the dispatcher.
func TestGenerateDangEntrypointSharedClients(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			ModuleName:      "smoke-mod",
			Runtime:         "node",
			ModulePath:      ".dagger/modules/smoke",
			ClientsDir:      ".dagger/clients",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Contains(t, got,
		`withMountedDirectory("node_modules/@dagger.io", workspace.directory("/.dagger/clients"))`)
	require.NotContains(t, got, `node_modules/@dagger.io/dagger`,
		"one mount of the shared directory replaces the per-package sdk mount")

	require.Contains(t, got, `let clients = workspace.directory("/.dagger/clients")`)
	for _, f := range []string{"__dagger.dispatch.ts", "loader.gen.ts", "tsconfig.json"} {
		require.Contains(t, got, `dir.exists("`+f+`")`)
	}
	for _, f := range []string{"dagger/index.ts", "dagger/client.gen.ts", "dagger/core.js"} {
		require.Contains(t, got, `clients.exists("`+f+`")`)
	}
	require.NotContains(t, got, `dir.exists("sdk/`,
		"the module carries no library of its own under the shared layout")
	require.Contains(t, got, `\".dagger/clients/dagger/index.ts\" is missing`,
		"a missing shared file is named by its workspace path, where the fix happens")
}

// TestGenerateDangEntrypointImplementsContract pins the shape the dang driver
// requires, all of which it rejects the module for getting wrong: exactly one
// type implementing ModuleEntrypoint, constructible with no arguments, and
// without re-declaring the interface — the driver writes that into the directory
// it loads, so a second declaration collides.
func TestGenerateDangEntrypointImplementsContract(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Equal(t, 1, strings.Count(got, "implements ModuleEntrypoint"))
	require.Contains(t, got, "type Entrypoint implements ModuleEntrypoint {")
	require.NotContains(t, got, "interface ModuleEntrypoint")

	// No `let` field: a field, even a defaulted one, becomes a constructor
	// argument, and the driver requires a zero-argument constructor.
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "let ") {
			require.Contains(t, trimmed, "(", "field %q would become a constructor argument", trimmed)
		}
	}

	require.Contains(t, got, "pub types(workspace: Workspace!): [TypeDef!]! {")
	require.Contains(t, got, "pub call(")
}

func TestGenerateDangEntrypoint_RequiresTypedef(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{},
	}}

	_, err := gen.GenerateDangEntrypoint(context.Background())
	require.ErrorContains(t, err, "TypedefJSONPath is required")
}

// TestGenerateDangEntrypointGuardsGeneratedFiles pins the guard call() runs
// before it builds anything.
//
// The engine's builtin TypeScript runtime has the same check, but under an
// entrypoint nothing runs that runtime — this program builds the container
// itself. Without the guard a gitignored sdk/ surfaces as a module-resolution
// error from inside a container, several layers from the cause.
func TestGenerateDangEntrypointGuardsGeneratedFiles(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		wants   []string
		absent  string
	}{
		{runtime: "node", wants: []string{"tsconfig.json"}, absent: "deno.json"},
		{runtime: "bun", wants: nil, absent: "tsconfig.json"},
		{runtime: "deno", wants: []string{"deno.json"}, absent: "tsconfig.json"},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					ModuleName:      "smoke-mod",
					Runtime:         tc.runtime,
					ModulePath:      ".dagger/modules/smoke",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			// Called from call(), never from types(): a module whose generated
			// tree is missing still lists its functions, then fails on the first
			// real call with something that names the file and the fix.
			require.Contains(t, got, "let checked = requireGenerated(workspace)")
			require.Contains(t, got, `workspace.directory("/.dagger/modules/smoke")`)

			// Every file the recipe mounts or execs.
			for _, f := range append([]string{
				"__dagger.dispatch.ts", "sdk/index.ts", "sdk/client.gen.ts", "sdk/core.js",
			}, tc.wants...) {
				require.Contains(t, got, `dir.exists("`+f+`")`)
			}
			require.NotContains(t, got, `dir.exists("`+tc.absent+`")`)

			// The workspace's name for the module, not the typedef's pascalized
			// object name.
			require.Contains(t, got, `module \"smoke-mod\" cannot run`)
			require.Contains(t, got, "Run `dagger generate`")
		})
	}
}
