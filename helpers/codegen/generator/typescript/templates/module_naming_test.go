package templates

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The module namer predicts the names the engine gives a module's
// declarations by strcase, the engine's legacy rules.
func TestModuleNamer(t *testing.T) {
	for _, test := range []struct {
		what                    string
		moduleName, typedefName string
		f                       func(moduleNamer) string
		want                    string
	}{
		{"object HttpClient", "naming-app", "NamingApp", func(n moduleNamer) string { return n.objectName("HttpClient") }, "HttpClient"},
		// strcase writes a run of capitals like a word: the entrypoints used to
		// send getURL as declared.
		{"field getURL", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("getURL") }, "getUrl"},
		{"field getUrl", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("getUrl") }, "getUrl"},
		{"field E2ETest", "naming-app", "NamingApp", func(n moduleNamer) string { return n.fieldName("E2ETest") }, "e2Etest"},
		{"iface HttpFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("HttpFetcher") }, "NamingAppHttpFetcher"},
		// The entrypoints used to keep the acronym run.
		{"iface HTTPFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("HTTPFetcher") }, "NamingAppHttpfetcher"},
		// The engine doesn't repeat the module's name; the entrypoints used to.
		{"iface NamingAppHttpFetcher of naming-app", "naming-app", "NamingApp", func(n moduleNamer) string { return n.namespaceObject("NamingAppHttpFetcher") }, "NamingAppHttpFetcher"},
		{"iface PondHttpClient of pond", "pond", "Pond", func(n moduleNamer) string { return n.namespaceObject("PondHttpClient") }, "PondHttpClient"},
		// The module's name prefixes a type on a word boundary only.
		{"iface Postman of post", "post", "Post", func(n moduleNamer) string { return n.namespaceObject("Postman") }, "PostPostman"},
		{"iface PostMan of post", "post", "Post", func(n moduleNamer) string { return n.namespaceObject("PostMan") }, "PostMan"},
		// The module's own name, which the typedef only carries as the
		// scanner's PascalCase.
		{"iface Tools of k8s-tools", "k8s-tools", "K8STools", func(n moduleNamer) string { return n.namespaceObject("Tools") }, "K8SToolsTools"},
		{"iface Go of my2go", "my2go", "My2Go", func(n moduleNamer) string { return n.namespaceObject("Go") }, "My2GoGo"},
		{"iface My2GoFetcher of my2go", "my2go", "My2Go", func(n moduleNamer) string { return n.namespaceObject("My2GoFetcher") }, "My2GoFetcher"},
	} {
		t.Run(test.what, func(t *testing.T) {
			require.Equal(t, test.want, test.f(moduleNamer{moduleName: test.moduleName, typedefName: test.typedefName}))
		})
	}
}

// The module's main object is the object whose name normalizes like the
// module's: strcase spells k8s-tools K8sTools, as this SDK's templates name
// the main class.
func TestModuleNamerMainObject(t *testing.T) {
	n := moduleNamer{moduleName: "k8s-tools", typedefName: "K8STools"}
	require.True(t, n.isMainObject("K8sTools"))
	require.False(t, n.isMainObject("K8STools"))
	require.False(t, n.isMainObject("Tools"))
}
