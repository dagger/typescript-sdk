package typescriptgenerator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/identifiers"
	"codegen/identifiers/identifierstest"
)

// recordedEngine answers formatIdentifiers as the engine at v1.0.0-beta.22
// (dagger/dagger naming/client-version-arg, 168f78a) does, from the table the
// templates' tests share. A name the table lacks fails the render, so a test
// learns of every name the generator predicts.
func recordedEngine(t *testing.T) *identifierstest.Engine {
	t.Helper()
	table, err := identifierstest.Load("templates/testdata/identifiers-v1.0.0-beta.22.json")
	require.NoError(t, err)
	return &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
}

// namesAt returns the names of the module's declarations at version, through
// engine; nil when version is before the cut.
func namesAt(t *testing.T, engine *identifierstest.Engine, version string) *identifiers.Names {
	t.Helper()
	names, err := engine.Connector().Names(t.Context(), version)
	require.NoError(t, err)
	return names
}

func renderNamingEntrypoint(t *testing.T, dispatch bool, names *identifiers.Names) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    dispatch,
			ModuleName:      "naming-app",
			Names:           names,
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
// dispatcher: by its rules for a module past the cut, by the strcase rules
// before it, without asking the engine.
func TestGenerateEntrypointInterfaceNames(t *testing.T) {
	for _, dispatch := range []bool{false, true} {
		engine := recordedEngine(t)
		legacy := renderNamingEntrypoint(t, dispatch, nil)
		require.Contains(t, legacy, `selectNode(id, "NamingAppHttpFetcher")`)
		// strcase writes getURL getUrl; it used to be sent as declared.
		require.Contains(t, legacy, `this._ctx.select("getUrl", __args)`)

		// A module named by a version before the cut keeps the legacy names.
		require.Equal(t, legacy, renderNamingEntrypoint(t, dispatch, namesAt(t, engine, "v1.0.0-beta.20")))
		require.Zero(t, engine.FormatQueries())

		got := renderNamingEntrypoint(t, dispatch, namesAt(t, engine, "v1.0.0-beta.22"))
		require.Contains(t, got, `selectNode(id, "NamingAppHTTPFetcher")`)
		require.Contains(t, got, `this._ctx.select("getUrl", __args)`)
		// The method keeps the name the user declared.
		require.Contains(t, got, "async getURL(path: any, arch: any)")
		require.Equal(t, 1, engine.FormatQueries(), "prefetched: one round trip")

		// An argument of the module's own enum is sent as the engine's name
		// for the member, and a result turned back into the TS value.
		require.Contains(t, legacy, `__args.__metadata = { "arch": { is_enum: true, value_to_name: (v: string) => ({ "amd64": "AMD_64", "arm64": "ARM_64" } as Record<string, string>)[v] ?? v } }`)
		require.Contains(t, got, `__args.__metadata = { "arch": { is_enum: true, value_to_name: (v: string) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v } }`)
		require.Contains(t, legacy, `const __name = await this._ctx.select("arch", __args).execute<any>()
    return ({ "AMD_64": "amd64", "ARM_64": "arm64" } as Record<string, string>)[__name] ?? __name`)
		require.Contains(t, got, `return ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__name] ?? __name`)
		// Lists of them too, element by element.
		require.Contains(t, got, `__args.__metadata = { "want": { is_enum: true, value_to_name: (vs: string[]) => vs.map((v) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v) } }`)
		require.Contains(t, got, `return (__name as string[]).map((__m) => ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__m] ?? __m)`)
	}
}

func renderDangNaming(t *testing.T, typedefPath, moduleName string, names *identifiers.Names) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: typedefPath,
			ModuleName:      moduleName,
			Runtime:         "node",
			ModulePath:      ".",
			ClientVersion:   "v1.0.0-beta.20",
			Names:           names,
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
// the one the engine would take for it by name: by its rules past the cut
// (GitHubTools for module github-tools, which strcase spells GithubTools; MY2Go
// for my2go, flat), by strcase before it (K8sTools for k8s-tools, as this
// SDK's templates name the class, which was left without one). Failing that,
// the one whose name pascalizes like the scanner's PascalCase of the module's
// name, which is how it was picked before (K8STools for k8s-tools).
func TestGenerateDangEntrypointMainObjectNames(t *testing.T) {
	for _, test := range []struct {
		module, typedefName string
		classes             []string
		legacy, engine      []string
	}{
		{"github-tools", "GithubTools", []string{"GitHubTools", "Tools"}, nil, []string{"GitHubTools"}},
		{"my2go", "My2Go", []string{"MY2Go", "Go"}, []string{"MY2Go"}, []string{"MY2Go"}},
		{"my2go", "My2Go", []string{"My2go", "Go"}, []string{"My2go"}, []string{"My2go"}},
		{"k8s-tools", "K8STools", []string{"K8sTools", "Tools"}, []string{"K8sTools"}, []string{"K8sTools"}},
		{"k8s-tools", "K8STools", []string{"K8STools", "Tools"}, []string{"K8STools"}, []string{"K8STools"}},
		{"naming-app", "NamingApp", []string{"NamingApp", "HttpClient"}, []string{"NamingApp"}, []string{"NamingApp"}},
	} {
		typedef := writeTypedef(t, test.typedefName, test.classes...)
		engine := recordedEngine(t)
		legacy := renderDangNaming(t, typedef, test.module, nil)
		require.Equal(t, test.legacy, constructed(legacy), "legacy %s %v", test.module, test.classes)
		require.Equal(t, legacy, renderDangNaming(t, typedef, test.module, namesAt(t, engine, "v1.0.0-beta.20")))
		got := renderDangNaming(t, typedef, test.module, namesAt(t, engine, "v1.0.0-beta.22"))
		require.Equal(t, test.engine, constructed(got), "engine %s %v", test.module, test.classes)
	}
}

// An engine error fails the render rather than falling back to names the
// engine may not use.
func TestGenerateEntrypointNamesEngineError(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: identifierstest.Table{}}
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			ModuleName:      "naming-app",
			Names:           namesAt(t, engine, "v1.0.0-beta.22"),
		},
	}}
	_, err := gen.GenerateEntrypoint(context.Background())
	require.ErrorContains(t, err, "fake engine: no")
}
