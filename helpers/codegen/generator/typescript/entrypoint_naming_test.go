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
		TypeNames:       map[string]string{"HttpFetcher": "NamingAppHTTPFetcher"},
		FieldNames:      map[string]string{"getURL": "getUrl", "path": "path", "arch": "arch", "archs": "archs", "want": "want"},
		EnumMemberNames: map[string]string{"Amd64": "AMD64", "Arm64": "ARM64"},
		MainObject:      "NamingApp",
	}
}

// smokeNaming answers for testdata/typedef_smoke.json, and ifaceReturnsNaming
// for testdata/typedef_iface_returns.json, as the engine does for a module
// named by strcase, which the goldens were rendered with.
func smokeNaming() generator.ModuleNaming {
	return naming(&namingtest.Engine{
		TypeNames:       map[string]string{"Greeter": "SmokeGreeter"},
		FieldNames:      map[string]string{"greet": "greet", "name": "name"},
		EnumMemberNames: map[string]string{"Loud": "LOUD", "Quiet": "QUIET"},
		MainObject:      "Smoke",
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

		// An argument of the module's own enum is sent as the engine's name
		// for the member, and a result turned back into the TS value.
		require.Contains(t, got, `__args.__metadata = { "arch": { is_enum: true, value_to_name: (v: string) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v } }`)
		require.Contains(t, got, `const __name = await this._ctx.select("arch", __args).execute<any>()
    return ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__name] ?? __name`)
		// Lists of them too, element by element, and a null as it is.
		require.Contains(t, got, `__args.__metadata = { "want": { is_enum: true, value_to_name: (vs: string[]) => vs == null ? vs : vs.map((v) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v) } }`)
		require.Contains(t, got, `return __name == null ? __name : (__name as string[]).map((__m) => ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__m] ?? __m)`)
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
// one; MY2Go for my2go, by the engine's flat match.
func TestGenerateDangEntrypointMainObjectNames(t *testing.T) {
	for _, test := range []struct {
		typedefName string
		classes     []string
		mainObject  string
	}{
		{"K8STools", []string{"K8sTools", "Tools"}, "K8sTools"},
		{"GithubTools", []string{"GitHubTools", "Tools"}, "GitHubTools"},
		{"My2Go", []string{"MY2Go", "Go"}, "MY2Go"},
	} {
		got := renderDangNaming(t, writeTypedef(t, test.typedefName, test.classes...), &namingtest.Engine{MainObject: test.mainObject})
		require.Equal(t, []string{test.mainObject}, constructed(got), "%s %v", test.typedefName, test.classes)
	}
}

// A module whose objects the engine takes none of for the main one fails
// generation: it would load with no constructor, and not surface. That
// includes K8STools in k8s-tools named by strcase, which the scanner's
// PascalCase of the module's name used to pick (strcase spells the module
// K8sTools, and the legacy rules have no flat match).
func TestGenerateDangEntrypointWithoutMainObject(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: writeTypedef(t, "K8STools", "K8STools", "Tools"),
			ModuleName:      "k8s-tools",
			Runtime:         "node",
			ModulePath:      ".",
			ClientVersion:   "v1.0.0-beta.20",
			Naming:          naming(&namingtest.Engine{}),
		},
	}}
	_, err := gen.GenerateDangEntrypoint(context.Background())
	require.EqualError(t, err, `module "k8s-tools" has no main object: the engine takes none of K8STools, Tools for it (ModuleSource.naming's mainObject: a class whose name the engine's naming rules for the module turn into the same type name as the module's)`)

	// A module that declares no object has none to take.
	got := renderDangNaming(t, writeTypedef(t, "Empty"), &namingtest.Engine{})
	require.Empty(t, constructed(got))
}
