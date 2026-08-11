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
