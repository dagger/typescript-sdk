package typescriptgenerator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

// namePairs is one table of the names the SDK hands codegen: the names asked
// about and the engine's answers, in the same order.
type namePairs struct {
	Names     []string `json:"names"`
	Formatted []string `json:"formatted"`
}

// writeNames writes the names the SDK got from ModuleSource.naming.
func writeNames(t *testing.T, names map[string]any) string {
	t.Helper()
	data, err := json.Marshal(names)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "names.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// namingAppNames are ModuleSource.naming's answers for
// testdata/typedef_naming.json at v1.0.0-beta.22, as the engine's Namer
// (dagger/dagger naming/client-version-arg, 168f78a) names them.
func namingAppNames(t *testing.T) string {
	return writeNames(t, map[string]any{
		"typeNames":       namePairs{[]string{"HttpFetcher"}, []string{"NamingAppHTTPFetcher"}},
		"fieldNames":      namePairs{[]string{"arch", "archs", "getURL", "path", "want"}, []string{"arch", "archs", "getUrl", "path", "want"}},
		"enumMemberNames": namePairs{[]string{"Amd64", "Arm64"}, []string{"AMD64", "ARM64"}},
		"mainObject":      "NamingApp",
	})
}

func renderNamingEntrypoint(t *testing.T, dispatch bool, namesPath string) (string, error) {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_naming.json",
			ModuleRoot:      "/work",
			SDKImportPath:   "@dagger.io/dagger",
			SourceDir:       "src",
			DispatchMode:    dispatch,
			NamesJSONPath:   namesPath,
		},
	}}
	state, err := gen.GenerateEntrypoint(context.Background())
	if err != nil {
		return "", err
	}
	file := DefaultEntrypointFile
	if dispatch {
		file = DefaultDispatchFile
	}
	return readOverlay(t, state, file), nil
}

// Calls through the module's own interfaces select the names the engine
// gives the module's declarations, in both the [runtime] entrypoint and the
// dispatcher.
func TestGenerateEntrypointInterfaceNames(t *testing.T) {
	for _, dispatch := range []bool{false, true} {
		got, err := renderNamingEntrypoint(t, dispatch, namingAppNames(t))
		require.NoError(t, err)
		require.Contains(t, got, `selectNode(id, "NamingAppHTTPFetcher")`)
		require.Contains(t, got, `this._ctx.select("getUrl", __args)`)
		// The method keeps the name the user declared.
		require.Contains(t, got, "async getURL(path: any, arch: any)")

		// An argument of the module's own enum is sent as the engine's name
		// for the member, and a result turned back into the TS value.
		require.Contains(t, got, `__args.__metadata = { "arch": { is_enum: true, value_to_name: (v: string) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v } }`)
		require.Contains(t, got, `return ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__name] ?? __name`)
		// Lists of them too, element by element.
		require.Contains(t, got, `__args.__metadata = { "want": { is_enum: true, value_to_name: (vs: string[]) => vs.map((v) => ({ "amd64": "AMD64", "arm64": "ARM64" } as Record<string, string>)[v] ?? v) } }`)
		require.Contains(t, got, `return (__name as string[]).map((__m) => ({ "AMD64": "amd64", "ARM64": "arm64" } as Record<string, string>)[__m] ?? __m)`)
	}
}

// A name the engine wasn't asked about fails the render rather than being
// guessed: the engine may not use it.
func TestGenerateEntrypointUnansweredName(t *testing.T) {
	_, err := renderNamingEntrypoint(t, true, "")
	require.ErrorContains(t, err, "the engine wasn't asked about Amd64, Arm64, HttpFetcher")
}

func renderDangNaming(t *testing.T, typedefPath, moduleName, namesPath string) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: typedefPath,
			ModuleName:      moduleName,
			Runtime:         "node",
			ModulePath:      ".",
			ClientVersion:   "v1.0.0-beta.20",
			NamesJSONPath:   namesPath,
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
// the one the engine would take for it by name (ModuleNaming.mainObject,
// here as the engine at v1.0.0-beta.22 answers). Failing that, the one whose
// name pascalizes like the scanner's PascalCase of the module's name, which
// is how it was picked before.
func TestGenerateDangEntrypointMainObjectNames(t *testing.T) {
	for _, test := range []struct {
		module, typedefName string
		classes             []string
		mainObject          any
		want                []string
	}{
		{"github-tools", "GithubTools", []string{"GitHubTools", "Tools"}, "GitHubTools", []string{"GitHubTools"}},
		{"my2go", "My2Go", []string{"MY2Go", "Go"}, "MY2Go", []string{"MY2Go"}},
		{"k8s-tools", "K8STools", []string{"K8sTools", "Tools"}, "K8sTools", []string{"K8sTools"}},
		// No answer falls back to the scanner's PascalCase.
		{"k8s-tools", "K8STools", []string{"K8STools", "Tools"}, nil, []string{"K8STools"}},
		{"github-tools", "GithubTools", []string{"GitHubTools", "Tools"}, nil, nil},
	} {
		typedef := writeTypedef(t, test.typedefName, test.classes...)
		got := renderDangNaming(t, typedef, test.module, writeNames(t, map[string]any{"mainObject": test.mainObject}))
		require.Equal(t, test.want, constructed(got), "%s %v", test.module, test.classes)
	}
}
