package templates

import (
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/identifiers"
	"codegen/identifiers/identifierstest"
)

// recordedNames are formatted as the engine at v1.0.0-beta.22 (dagger/dagger
// naming/client-version-arg, 168f78a) formats them:
// testdata/identifiers-v1.0.0-beta.22.json records its formatIdentifiers
// results. A name the table lacks fails the test.
func recordedNames(t *testing.T) *identifiers.Names {
	t.Helper()
	table, err := identifierstest.Load("testdata/identifiers-v1.0.0-beta.22.json")
	require.NoError(t, err)
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names, err := engine.Connector().Names(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.NotNil(t, names)
	return names
}

// formatted runs f until every name it looks up is formatted (see
// identifiers.Names.Run), and returns what it returned last.
func formatted[T any](t *testing.T, names *identifiers.Names, f func() T) T {
	t.Helper()
	var got T
	require.NoError(t, names.Run(t.Context(), func() error {
		got = f()
		return nil
	}))
	return got
}

// The module namer predicts the names the engine gives a module's
// declarations: by its rules past the cut, by strcase, the engine's legacy
// rules, before it. The legacy predictions are made locally; they never ask
// the engine.
func TestModuleNamer(t *testing.T) {
	names := recordedNames(t)
	type prediction struct{ legacy, engine string }
	for _, test := range []struct {
		what                    string
		moduleName, typedefName string
		f                       func(moduleNamer) string
		want                    prediction
	}{
		{"object HttpClient", "naming-app", "NamingApp", func(n moduleNamer) string { return n.objectName("HttpClient") }, prediction{"HttpClient", "HTTPClient"}},
		// strcase writes a run of capitals like a word, as the new rules do
		// for fields: the entrypoints used to send getURL as declared.
		{"field getURL", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("getURL") }, prediction{"getUrl", "getUrl"}},
		{"field getUrl", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("getUrl") }, prediction{"getUrl", "getUrl"}},
		{"field E2ETest", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("E2ETest") }, prediction{"e2Etest", "e2eTest"}},
		{"member Arm64", "naming-app", "NamingApp", func(n moduleNamer) string { return n.enumMemberName("Arm64") }, prediction{"ARM_64", "ARM64"}},
		{"member InProgress", "naming-app", "NamingApp", func(n moduleNamer) string { return n.enumMemberName("InProgress") }, prediction{"IN_PROGRESS", "IN_PROGRESS"}},
		{"member GitHubSsh", "naming-app", "NamingApp", func(n moduleNamer) string { return n.enumMemberName("GitHubSsh") }, prediction{"GIT_HUB_SSH", "GITHUB_SSH"}},
		// Names already in GraphQL's member convention are kept.
		{"member HTTP2", "naming-app", "NamingApp", func(n moduleNamer) string { return n.enumMemberName("HTTP2") }, prediction{"HTTP2", "HTTP2"}},
		{"member P_256", "naming-app", "NamingApp", func(n moduleNamer) string { return n.enumMemberName("P_256") }, prediction{"P_256", "P_256"}},
		{"iface HttpFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("HttpFetcher") }, prediction{"NamingAppHttpFetcher", "NamingAppHTTPFetcher"}},
		// strcase writes an acronym run like a word; the entrypoints used to
		// keep it.
		{"iface HTTPFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("HTTPFetcher") }, prediction{"NamingAppHttpfetcher", "NamingAppHTTPFetcher"}},
		// Neither rules repeat the module's name; the entrypoints used to.
		{"iface NamingAppHttpFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("NamingAppHttpFetcher") }, prediction{"NamingAppHttpFetcher", "NamingAppHTTPFetcher"}},
		{"iface PondHttpClient of pond", "pond", "Pond", func(n moduleNamer) string { return n.namespaceObject("PondHttpClient") }, prediction{"PondHttpClient", "PondHTTPClient"}},
		// The module's name prefixes a type on a word boundary only.
		{"iface Postman of post", "post", "Post", func(n moduleNamer) string { return n.namespaceObject("Postman") }, prediction{"PostPostman", "PostPostman"}},
		{"iface PostMan of post", "post", "Post", func(n moduleNamer) string { return n.namespaceObject("PostMan") }, prediction{"PostMan", "PostMan"}},
		// The module's own name, not the scanner's PascalCase of it: the
		// engine cuts k8s-tools and my2go into words differently than
		// K8STools and My2Go.
		{"iface Tools of k8s-tools", "k8s-tools", "K8STools", func(n moduleNamer) string { return n.namespaceObject("Tools") }, prediction{"K8SToolsTools", "K8sToolsTools"}},
		{"iface K8STools of k8s-tools", "k8s-tools", "K8STools", func(n moduleNamer) string { return n.namespaceObject("K8STools") }, prediction{"K8SToolsK8Stools", "K8sTools"}},
		{"iface Go of my2go", "my2go", "My2Go", func(n moduleNamer) string { return n.namespaceObject("Go") }, prediction{"My2GoGo", "My2goGo"}},
		// A flat prefix still counts as the module's name.
		{"iface My2GoFetcher of my2go", "my2go", "My2Go", func(n moduleNamer) string { return n.namespaceObject("My2GoFetcher") }, prediction{"My2GoFetcher", "My2goFetcher"}},
	} {
		t.Run(test.what, func(t *testing.T) {
			legacy := moduleNamer{moduleName: test.moduleName, typedefName: test.typedefName}
			engine := moduleNamer{names: names, moduleName: test.moduleName, typedefName: test.typedefName}
			got := prediction{
				legacy: test.f(legacy),
				engine: formatted(t, names, func() string { return test.f(engine) }),
			}
			require.Equal(t, test.want, got)
		})
	}
}

// The module's main object is the object named like the module, or else the
// only one named like it once flattened (K8STools for k8s-tools).
func TestModuleNamerMainObject(t *testing.T) {
	names := recordedNames(t)
	isMain := func(moduleName, typedefName, name string, objects ...string) bool {
		n := moduleNamer{names: names, moduleName: moduleName, typedefName: typedefName}
		return formatted(t, names, func() bool { return n.isMainObject(name, objects) })
	}
	require.True(t, isMain("naming-app", "NamingApp", "NamingApp", "NamingApp", "HttpClient"))
	require.False(t, isMain("naming-app", "NamingApp", "HttpClient", "NamingApp", "HttpClient"))
	require.True(t, isMain("k8s-tools", "K8STools", "K8STools", "K8STools", "Tools"))
	require.True(t, isMain("k8s-tools", "K8STools", "K8sTools", "K8sTools", "Tools"))
	require.True(t, isMain("my2go", "My2Go", "My2Go", "My2Go"))
	require.False(t, isMain("my2go", "My2Go", "My2Go", "My2Go", "My2go"), "an exact match wins over a flat one")
	require.False(t, isMain("k8s-tools", "K8STools", "Tools", "K8STools", "Tools"))

	// The legacy rule is strcase's, which spells k8s-tools K8sTools, as this
	// SDK's templates name the main class; it has no flat match.
	legacy := moduleNamer{moduleName: "k8s-tools", typedefName: "K8STools"}
	require.True(t, legacy.isMainObject("K8sTools", []string{"K8sTools"}))
	require.False(t, legacy.isMainObject("K8STools", []string{"K8STools"}), "the legacy rule has no flat match")
}

// Prefetching a module's names formats everything its entrypoints predict in
// one query.
func TestPrefetchModuleNames(t *testing.T) {
	table, err := identifierstest.Load("testdata/identifiers-v1.0.0-beta.22.json")
	require.NoError(t, err)
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names, err := engine.Connector().Names(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	module := namingAppModule()
	PrefetchModuleNames(module, "naming-app", names)
	n := moduleNamer{names: names, moduleName: "naming-app", typedefName: module.Name}
	runs := 0
	require.NoError(t, names.Run(t.Context(), func() error {
		runs++
		n.namespaceObject("HttpFetcher")
		n.fieldName("getUrl")
		n.enumMemberName("Arm64")
		n.isMainObject("NamingApp", []string{"NamingApp", "HttpClient"})
		return nil
	}))
	require.Equal(t, 1, runs)
	require.Equal(t, 1, engine.FormatQueries())
}

// namingAppModule declares what the engine's rules name differently from the
// legacy ones: an interface whose name has an acronym, called through with an
// argument of an enum whose members have digits.
func namingAppModule() *TypedefModule {
	str := &TypedefType{Kind: KindString}
	return &TypedefModule{
		Name: "NamingApp",
		Objects: map[string]*TypedefObject{
			"NamingApp": {
				Name:       "NamingApp",
				Kind:       "class",
				IsExported: true,
				Location:   &TypedefLocation{Filepath: "/src/src/index.ts"},
				Methods: map[string]*TypedefFunction{
					"fetch": {
						Name:       "fetch",
						ReturnType: str,
						Arguments: []*TypedefArgument{
							{Name: "fetcher", Type: &TypedefType{Kind: KindInterface, Name: "HttpFetcher"}},
							{Name: "path", Type: str},
						},
					},
				},
				Properties: map[string]*TypedefProperty{},
			},
			"HttpClient": {
				Name:       "HttpClient",
				Kind:       "class",
				IsExported: true,
				Location:   &TypedefLocation{Filepath: "/src/src/index.ts"},
				Methods: map[string]*TypedefFunction{
					"getUrl": {Name: "getUrl", ReturnType: str, Arguments: []*TypedefArgument{{Name: "path", Type: str}}},
				},
				Properties: map[string]*TypedefProperty{},
			},
		},
		Enums: map[string]*TypedefEnum{
			"Arch": {
				Name: "Arch",
				Values: map[string]*TypedefEnumValue{
					"Arm64": {Name: "Arm64", Value: "arm64"},
					"Amd64": {Name: "Amd64", Value: "amd64"},
				},
			},
		},
		Interfaces: map[string]*TypedefInterface{
			"HttpFetcher": {
				Name: "HttpFetcher",
				Functions: map[string]*TypedefFunction{
					"getUrl": {
						Name:       "getUrl",
						ReturnType: str,
						Arguments:  []*TypedefArgument{{Name: "path", Type: str}},
					},
				},
			},
		},
	}
}
