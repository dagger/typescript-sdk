package typescriptgenerator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/namingtest"
)

// naming asks engine about the module source with ID "src".
func naming(engine *namingtest.Engine) generator.ModuleNaming {
	engine.SourceID = "src"
	return generator.ModuleNaming{Engine: engine, SourceID: "src"}
}

// namingAppEngine answers for testdata/typedef_naming.json as the engine at
// v1.0.0-beta.22 does for a module named by its identifier naming rules.
func namingAppEngine() *namingtest.Engine {
	return &namingtest.Engine{
		TypeNames:  map[string]string{"HttpFetcher": "NamingAppHTTPFetcher"},
		FieldNames: map[string]string{"getURL": "getUrl", "path": "path", "arch": "arch"},
		MainObject: "NamingApp",
	}
}

// smokeNaming answers for testdata/typedef_smoke.json, and ifaceReturnsNaming
// for testdata/typedef_iface_returns.json, as the engine does for a module
// named by strcase, which the goldens were rendered with.
func smokeNaming() generator.ModuleNaming {
	return naming(&namingtest.Engine{
		TypeNames:  map[string]string{"Greeter": "SmokeGreeter"},
		FieldNames: map[string]string{"greet": "greet", "name": "name"},
		MainObject: "Smoke",
	})
}

func ifaceReturnsNaming() generator.ModuleNaming {
	return naming(&namingtest.Engine{
		TypeNames:  map[string]string{"Greeter": "IfacesGreeter", "Speaker": "IfacesSpeaker"},
		FieldNames: map[string]string{"chain": "chain", "ctr": "ctr", "ctrs": "ctrs", "say": "say", "speaker": "speaker", "widget": "widget"},
		MainObject: "Ifaces",
	})
}

func renderNamingEntrypoint(t *testing.T, dispatch bool, engine *namingtest.Engine) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    dispatch,
			Naming:          naming(engine),
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
// dispatcher, asked in one query. The TS method keeps the name the user
// declared.
func TestGenerateEntrypointInterfaceNames(t *testing.T) {
	for _, dispatch := range []bool{false, true} {
		engine := namingAppEngine()
		got := renderNamingEntrypoint(t, dispatch, engine)
		require.Contains(t, got, `selectNode(id, "NamingAppHTTPFetcher")`)
		require.Contains(t, got, `this._ctx.select("getUrl", __args)`)
		require.Contains(t, got, "async getURL(path: any, arch: any)")
		require.Equal(t, 1, engine.Queries())
	}
}

// A name the engine can't give fails generation instead of being guessed.
func TestGenerateEntrypointNamingError(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			Naming:          naming(&namingtest.Engine{}),
		},
	}}
	_, err := gen.GenerateEntrypoint(context.Background())
	require.ErrorContains(t, err, `can't name "HttpFetcher"`)
}

func renderDangNaming(t *testing.T, typedefPath string, engine *namingtest.Engine) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: typedefPath,
			ModuleName:      "app",
			Runtime:         "node",
			ModulePath:      ".",
			ClientVersion:   "v1.0.0-beta.20",
			Naming:          naming(engine),
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
// the one the engine takes for it (ModuleNaming.mainObject): K8sTools for
// k8s-tools, as this SDK's templates name the class, which was left without
// one. When the engine takes none, the one whose name pascalizes like the
// scanner's PascalCase of the module's name gets it, as before.
func TestGenerateDangEntrypointMainObjectNames(t *testing.T) {
	for _, test := range []struct {
		typedefName string
		classes     []string
		mainObject  string
		want        []string
	}{
		{"K8STools", []string{"K8sTools", "Tools"}, "K8sTools", []string{"K8sTools"}},
		{"GithubTools", []string{"GitHubTools", "Tools"}, "GitHubTools", []string{"GitHubTools"}},
		{"K8STools", []string{"K8STools", "Tools"}, "", []string{"K8STools"}},
		{"GithubTools", []string{"GitHubTools", "Tools"}, "", nil},
	} {
		got := renderDangNaming(t, writeTypedef(t, test.typedefName, test.classes...), &namingtest.Engine{MainObject: test.mainObject})
		require.Equal(t, test.want, constructed(got), "%s %v", test.typedefName, test.classes)
	}
}
