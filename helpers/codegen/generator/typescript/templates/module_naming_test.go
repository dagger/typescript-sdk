package templates

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/namingtest"
)

// Both sides of the cut use exactly the served source's context, including
// an alias distinct from its original name and the scanned typedef's name.
func TestLoadModuleNames(t *testing.T) {
	for _, tc := range []struct{ version, iface, field, member string }{
		{"v1.0.0-beta.25", "AliasHttpFetcher", "e2Etest", "ARM_64"},
		{"v1.0.0-beta.26", "AliasHTTPFetcher", "e2eTest", "ARM64"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			engine := &namingtest.Engine{
				Version: tc.version, Name: "alias", OriginalName: "naming-app",
				TypeNames:       map[string]string{"HttpFetcher": tc.iface},
				FieldNames:      map[string]string{"getUrl": "getUrl", "path": "path", "E2ETest": tc.field},
				EnumMemberNames: map[string]string{"Arm64": tc.member},
				MainObject:      "NamingApp",
			}
			naming := generator.ModuleNaming{Engine: engine, Version: tc.version, Name: "alias", OriginalName: "naming-app"}
			names, err := LoadModuleNames(t.Context(), naming, namingAppModule())
			require.NoError(t, err)
			require.Equal(t, 1, engine.Queries())
			require.Equal(t, "NamingApp", names.MainObject())

			got, err := names.typeName("HttpFetcher")
			require.NoError(t, err)
			require.Equal(t, tc.iface, got)
			got, err = names.fieldName("getUrl")
			require.NoError(t, err)
			require.Equal(t, "getUrl", got)
			got, err = names.enumMemberName("Arm64")
			require.NoError(t, err)
			require.Equal(t, tc.member, got)
			require.Equal(t, 1, engine.Queries(), "asked up front")

			// A miss uses the same naming context and is cached after one query.
			for range 2 {
				got, err = names.fieldName("E2ETest")
				require.NoError(t, err)
				require.Equal(t, tc.field, got)
			}
			require.Equal(t, 2, engine.Queries())
			_, err = names.typeName("Unknown")
			require.ErrorContains(t, err, `can't name "Unknown"`)
		})
	}
}

func TestLoadModuleNamesFails(t *testing.T) {
	engine := &namingtest.Engine{Version: "v1.0.0-beta.26", Name: "alias", OriginalName: "naming-app"}
	naming := generator.ModuleNaming{Engine: engine, Version: engine.Version, Name: engine.Name, OriginalName: engine.OriginalName}
	_, err := LoadModuleNames(t.Context(), naming, namingAppModule())
	require.ErrorContains(t, err, `can't name "HttpFetcher"`)

	for _, field := range []string{"version", "name", "originalName"} {
		missing := naming
		switch field {
		case "version":
			missing.Version = ""
		case "name":
			missing.Name = ""
		case "originalName":
			missing.OriginalName = ""
		}
		_, err := LoadModuleNames(t.Context(), missing, namingAppModule())
		require.ErrorContains(t, err, "no complete naming context")
	}

	for _, response := range []string{`{}`, `{"naming":null}`, `{"naming":{}}`, `{"naming":{"module":null}}`} {
		naming.Engine = namingQueryFunc(func(_ context.Context, _ string, _ map[string]any, data any) error {
			return json.Unmarshal([]byte(response), data)
		})
		_, err := LoadModuleNames(t.Context(), naming, namingAppModule())
		require.ErrorContains(t, err, "engine returned no module naming")
	}
}

type namingQueryFunc func(context.Context, string, map[string]any, any) error

func (f namingQueryFunc) Query(ctx context.Context, query string, variables map[string]any, data any) error {
	return f(ctx, query, variables, data)
}

// Without an engine nothing is asked, and a name looked up fails.
func TestModuleNamesWithoutEngine(t *testing.T) {
	names, err := LoadModuleNames(t.Context(), generator.ModuleNaming{}, namingAppModule())
	require.NoError(t, err)
	require.Empty(t, names.MainObject())
	_, err = names.typeName("HttpFetcher")
	require.ErrorContains(t, err, "no complete naming context")
}

func namingAppModule() *TypedefModule {
	str := &TypedefType{Kind: KindString}
	return &TypedefModule{
		Name: "NamingApp",
		Objects: map[string]*TypedefObject{
			"NamingApp":  {Name: "NamingApp", Kind: "class", IsExported: true, Methods: map[string]*TypedefFunction{}, Properties: map[string]*TypedefProperty{}},
			"HttpClient": {Name: "HttpClient", Kind: "class", IsExported: true, Methods: map[string]*TypedefFunction{}, Properties: map[string]*TypedefProperty{}},
		},
		Interfaces: map[string]*TypedefInterface{
			"HttpFetcher": {
				Name: "HttpFetcher",
				Functions: map[string]*TypedefFunction{
					"getUrl": {Name: "getUrl", ReturnType: str, Arguments: []*TypedefArgument{{Name: "path", Type: str}}},
				},
			},
		},
		Enums: map[string]*TypedefEnum{
			"Arch": {Name: "Arch", Values: map[string]*TypedefEnumValue{"Arm64": {Name: "Arm64", Value: "arm64"}}},
		},
	}
}
