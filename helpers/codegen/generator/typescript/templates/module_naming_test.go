package templates

import (
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/namingtest"
)

// The names a module's entrypoints select are asked for in one query, and
// looked up as the engine answered them.
func TestLoadModuleNames(t *testing.T) {
	engine := &namingtest.Engine{
		SourceID:   "src",
		TypeNames:  map[string]string{"HttpFetcher": "NamingAppHTTPFetcher"},
		FieldNames: map[string]string{"getUrl": "getUrl", "path": "path", "E2ETest": "e2eTest"},
		MainObject: "NamingApp",
	}
	names, err := LoadModuleNames(t.Context(), generator.ModuleNaming{Engine: engine, SourceID: "src"}, namingAppModule())
	require.NoError(t, err)
	require.Equal(t, 1, engine.Queries())
	require.Equal(t, "NamingApp", names.MainObject())

	got, err := names.typeName("HttpFetcher")
	require.NoError(t, err)
	require.Equal(t, "NamingAppHTTPFetcher", got)
	got, err = names.fieldName("getUrl")
	require.NoError(t, err)
	require.Equal(t, "getUrl", got)
	require.Equal(t, 1, engine.Queries(), "asked up front")

	// A name that wasn't asked up front is asked directly, once.
	for range 2 {
		got, err = names.fieldName("E2ETest")
		require.NoError(t, err)
		require.Equal(t, "e2eTest", got)
	}
	require.Equal(t, 2, engine.Queries())

	// A name the engine can't name fails.
	_, err = names.typeName("Unknown")
	require.ErrorContains(t, err, `can't name "Unknown"`)
}

// An engine that can't name what the module declares fails loading, and so
// does a source the engine doesn't know.
func TestLoadModuleNamesFails(t *testing.T) {
	engine := &namingtest.Engine{SourceID: "src"}
	_, err := LoadModuleNames(t.Context(), generator.ModuleNaming{Engine: engine, SourceID: "src"}, namingAppModule())
	require.ErrorContains(t, err, `can't name "HttpFetcher"`)

	_, err = LoadModuleNames(t.Context(), generator.ModuleNaming{Engine: engine, SourceID: "other"}, namingAppModule())
	require.ErrorContains(t, err, `"other" is not a module source`)
}

// Without a module source nothing is asked, and a name looked up fails.
func TestModuleNamesWithoutEngine(t *testing.T) {
	names, err := LoadModuleNames(t.Context(), generator.ModuleNaming{}, namingAppModule())
	require.NoError(t, err)
	require.Empty(t, names.MainObject())
	_, err = names.typeName("HttpFetcher")
	require.ErrorContains(t, err, "no module source")
}

// namingAppModule declares an interface whose name has an acronym, which the
// engine names differently by its rules than by strcase.
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
	}
}
