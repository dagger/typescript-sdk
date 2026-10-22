package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func renderEntrypoint(t *testing.T, module *TypedefModule, opts EntrypointOptions) string {
	t.Helper()
	opts.ModuleRoot = "/src"
	var b bytes.Buffer
	require.NoError(t, NewEntrypoint(module, opts).ExecuteTemplate(&b, "entrypoint", module))
	return b.String()
}

// pondModule declares an interface whose names the engine's naming rules
// recase: HttpFetcher.getUrl(comUrl).
func pondModule() *TypedefModule {
	str := &TypedefType{Kind: KindString}
	return &TypedefModule{
		Name: "Pond",
		Objects: map[string]*TypedefObject{
			"Pond": {
				Name:       "Pond",
				Kind:       "class",
				IsExported: true,
				Location:   &TypedefLocation{Filepath: "/src/src/index.ts"},
				Methods: map[string]*TypedefFunction{
					"hello": {Name: "hello", ReturnType: str},
				},
				Properties: map[string]*TypedefProperty{},
			},
		},
		Enums: map[string]*TypedefEnum{},
		Interfaces: map[string]*TypedefInterface{
			"HttpFetcher": {
				Name: "HttpFetcher",
				Functions: map[string]*TypedefFunction{
					"getUrl": {
						Name:       "getUrl",
						ReturnType: str,
						Arguments:  []*TypedefArgument{{Name: "comUrl", Type: str}},
					},
				},
			},
			"PondHttpClient": {
				Name: "PondHttpClient",
				Functions: map[string]*TypedefFunction{
					"withSshKey": {
						Name:       "withSshKey",
						ReturnType: &TypedefType{Kind: KindInterface, Name: "PondHttpClient"},
						Arguments:  []*TypedefArgument{{Name: "key", Type: str}},
					},
				},
			},
		},
	}
}

// A module named by the engine's naming rules (its schema has
// Query.formatIdentifiers) is called through its interfaces by the names the
// engine gives them; the TS methods keep the names the user declared.
func TestEntrypointInterfaceEngineNames(t *testing.T) {
	got := renderEntrypoint(t, pondModule(), EntrypointOptions{EngineNaming: true, NamingVersion: "v1.0.0-beta.21"})

	require.Contains(t, got, `selectNode(id, "PondHTTPFetcher")`)
	require.Contains(t, got, `__args["comURL"] = comUrl`)
	require.Contains(t, got, `this._ctx.select("getURL", __args)`)
	require.Contains(t, got, "async getUrl(comUrl: any)")

	// already namespaced: the module's words aren't repeated
	require.Contains(t, got, `selectNode(id, "PondHTTPClient")`)
	require.Contains(t, got, `this._ctx.select("withSSHKey", __args)`)

	// nor are they when the words are cut differently around digits
	my2go := &TypedefModule{
		Name:    "My2go",
		Objects: map[string]*TypedefObject{},
		Enums:   map[string]*TypedefEnum{},
		Interfaces: map[string]*TypedefInterface{
			"My2GoFetcher": {Name: "My2GoFetcher", Functions: map[string]*TypedefFunction{}},
		},
	}
	got = renderEntrypoint(t, my2go, EntrypointOptions{EngineNaming: true})
	require.Contains(t, got, `selectNode(id, "My2goFetcher")`)
}

// Older modules keep the names the entrypoint always used.
func TestEntrypointInterfaceLegacyNames(t *testing.T) {
	got := renderEntrypoint(t, pondModule(), EntrypointOptions{})

	require.Contains(t, got, `selectNode(id, "PondHttpFetcher")`)
	require.Contains(t, got, `__args["comUrl"] = comUrl`)
	require.Contains(t, got, `this._ctx.select("getUrl", __args)`)
	require.Contains(t, got, `this._ctx.select("withSshKey", __args)`)
}
