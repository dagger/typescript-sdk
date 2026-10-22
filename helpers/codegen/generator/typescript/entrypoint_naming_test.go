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

// moduleSchemaJSON is a module schema as the engine serves it: with
// Query.formatIdentifiers when gated (a v1.0.0-beta.21+ view).
func moduleSchemaJSON(t *testing.T, gated bool, version string) string {
	t.Helper()
	fields := `{"name": "container", "args": [], "type": {"kind": "OBJECT", "name": "Container"}}`
	if gated {
		fields += `, {"name": "formatIdentifiers", "args": [], "type": {"kind": "LIST", "ofType": {"kind": "SCALAR", "name": "String"}}}`
	}
	path := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
  "__schemaVersion": "`+version+`",
  "__schema": {
    "queryType": {"name": "Query"},
    "types": [{"kind": "OBJECT", "name": "Query", "fields": [`+fields+`]}],
    "directives": []
  }
}`), 0o600))
	return path
}

// pondTypedefJSON is a module declaring an interface whose names the engine's
// naming rules recase: HttpFetcher.getUrl(comUrl).
func pondTypedefJSON(t *testing.T) string {
	t.Helper()
	str := &templates.TypedefType{Kind: templates.KindString}
	module := templates.TypedefModule{
		Name: "Pond",
		Objects: map[string]*templates.TypedefObject{
			"Pond": {
				Name:       "Pond",
				Kind:       "class",
				IsExported: true,
				Location:   &templates.TypedefLocation{Filepath: "src/index.ts"},
				Methods: map[string]*templates.TypedefFunction{
					"hello": {Name: "hello", ReturnType: str},
				},
				Properties: map[string]*templates.TypedefProperty{},
			},
		},
		Enums: map[string]*templates.TypedefEnum{},
		Interfaces: map[string]*templates.TypedefInterface{
			"HttpFetcher": {
				Name: "HttpFetcher",
				Functions: map[string]*templates.TypedefFunction{
					"getUrl": {
						Name:       "getUrl",
						ReturnType: str,
						Arguments:  []*templates.TypedefArgument{{Name: "comUrl", Type: str}},
					},
				},
			},
		},
	}
	data, err := json.Marshal(module)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "typedef.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// The module schema is the naming gate for calls through the module's own
// interfaces, in both the legacy entrypoint and the dispatcher.
func TestGenerateEntrypointInterfaceNamingGate(t *testing.T) {
	typedef := pondTypedefJSON(t)
	for _, dispatch := range []bool{false, true} {
		render := func(schemaPath string) string {
			gen := &TypeScriptGenerator{Config: generator.Config{
				EntrypointConfig: &generator.EntrypointGeneratorConfig{
					TypedefJSONPath:       typedef,
					ModuleRoot:            "/work",
					SDKImportPath:         "@dagger.io/dagger",
					SourceDir:             "src",
					DispatchMode:          dispatch,
					IntrospectionJSONPath: schemaPath,
				},
			}}
			state, err := gen.GenerateEntrypoint(context.Background())
			require.NoError(t, err)
			file := DefaultEntrypointFile
			if dispatch {
				file = DefaultDispatchFile
			}
			return readOverlay(t, state, file)
		}

		legacy := render("")
		require.Contains(t, legacy, `selectNode(id, "PondHttpFetcher")`)
		require.Contains(t, legacy, `select("getUrl", __args)`)

		// a pre-cut module schema keeps the legacy names, byte for byte
		require.Equal(t, legacy, render(moduleSchemaJSON(t, false, "v1.0.0-beta.20")))

		// from the cut, the names the engine gives the module's declarations
		gated := render(moduleSchemaJSON(t, true, "v1.0.0-beta.21"))
		require.Contains(t, gated, `selectNode(id, "PondHTTPFetcher")`)
		require.Contains(t, gated, `__args["comURL"] = comUrl`)
		require.Contains(t, gated, `select("getURL", __args)`)
		require.Contains(t, gated, "async getUrl(comUrl: any)")
	}
}
