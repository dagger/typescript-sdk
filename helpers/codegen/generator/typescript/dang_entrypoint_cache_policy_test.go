package typescriptgenerator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/typescript/templates"
)

func TestGenerateDangEntrypointCachePolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cache  string
		policy string
	}{
		{"never", "never", "FunctionCachePolicy.NEVER"},
		{"session", "session", "FunctionCachePolicy.PER_SESSION"},
		{"ttl", "1h", `FunctionCachePolicy.DEFAULT, timeToLive: "1h"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := os.ReadFile("testdata/typedef_smoke.json")
			require.NoError(t, err)

			var module templates.TypedefModule
			require.NoError(t, json.Unmarshal(input, &module))
			for _, object := range module.Objects {
				for _, method := range object.Methods {
					method.Cache = tc.cache
				}
			}

			input, err = json.Marshal(module)
			require.NoError(t, err)
			path := filepath.Join(t.TempDir(), "typedef.json")
			require.NoError(t, os.WriteFile(path, input, 0o600))

			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: path,
					Naming:          smokeNaming(),
					ClientVersion:   "v1.0.0-beta.28",
					Runtime:         "node",
					ModulePath:      ".",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)
			require.Contains(t, got, ".withCachePolicy("+tc.policy+")")
		})
	}
}
