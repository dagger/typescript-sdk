package typescriptgenerator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

func renderNamingEntrypoint(t *testing.T, dispatch bool) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    dispatch,
			ModuleName:      "naming-app",
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

// Calls through the module's own interfaces select the names the engine
// gives the module's declarations, in both the [runtime] entrypoint and the
// dispatcher. strcase writes getURL getUrl, which used to be sent as declared;
// the TS method keeps the name the user declared.
func TestGenerateEntrypointInterfaceNames(t *testing.T) {
	for _, dispatch := range []bool{false, true} {
		got := renderNamingEntrypoint(t, dispatch)
		require.Contains(t, got, `selectNode(id, "NamingAppHttpFetcher")`)
		require.Contains(t, got, `this._ctx.select("getUrl", __args)`)
		require.Contains(t, got, "async getURL(path: any, arch: any)")
	}
}

func renderDangNaming(t *testing.T, typedefPath, moduleName string) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: typedefPath,
			ModuleName:      moduleName,
			Runtime:         "node",
			ModulePath:      ".",
			ClientVersion:   "v1.0.0-beta.20",
		},
	}}
	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	return readOverlay(t, state, DefaultDangEntrypointFile)
}

// writeTypedef writes a typedef of a module named typedefName whose objects
// are the classes named, with no members.
func writeTypedef(t *testing.T, typedefName string, classes ...string) string {
	t.Helper()
	objects := make([]string, len(classes))
	for i, class := range classes {
		objects[i] = `"` + class + `": {"name": "` + class + `", "kind": "class", "isExported": true, "methods": {}, "properties": {}}`
	}
	path := filepath.Join(t.TempDir(), "typedef.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"name": "`+typedefName+`", "objects": {`+
		strings.Join(objects, ", ")+`}, "enums": {}, "interfaces": {}}`), 0o600))
	return path
}

// constructed returns the objects a rendered main.dang gives a constructor.
func constructed(main string) []string {
	var got []string
	for _, part := range strings.Split(main, `withConstructor(
          function("", typeDef.withObject("`)[1:] {
		got = append(got, part[:strings.Index(part, `"`)])
	}
	return got
}

// The main object, the one the generated entrypoint gives a constructor, is
// the one the engine would take for it by name: K8sTools for k8s-tools, as
// this SDK's templates name the class, which was left without one. Failing
// that, the one whose name pascalizes like the scanner's PascalCase of the
// module's name, which is how it was picked before (K8STools for k8s-tools).
func TestGenerateDangEntrypointMainObjectNames(t *testing.T) {
	for _, test := range []struct {
		module, typedefName string
		classes             []string
		want                []string
	}{
		{"k8s-tools", "K8STools", []string{"K8sTools", "Tools"}, []string{"K8sTools"}},
		{"k8s-tools", "K8STools", []string{"K8STools", "Tools"}, []string{"K8STools"}},
		{"github-tools", "GithubTools", []string{"GitHubTools", "Tools"}, nil},
		{"naming-app", "NamingApp", []string{"NamingApp", "HttpClient"}, []string{"NamingApp"}},
	} {
		got := renderDangNaming(t, writeTypedef(t, test.typedefName, test.classes...), test.module)
		require.Equal(t, test.want, constructed(got), "%s %v", test.module, test.classes)
	}
}
