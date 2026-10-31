package typescriptgenerator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/namingtest"
	"codegen/generator/typescript/templates"
	"codegen/introspection"
)

// styling is an engine that styles the identifiers the bindings generate as
// the engine does (namingtest.Styles).
func styling() *namingtest.Engine {
	return &namingtest.Engine{Pascal: namingtest.Styles}
}

// identifiers styles through styling, each name as it is looked up.
func identifiers(t *testing.T) *templates.Identifiers {
	t.Helper()
	ids, err := templates.LoadIdentifiers(context.Background(), styling(), "v0.21.0", nil)
	require.NoError(t, err)
	return ids
}

// enumSchema is a core enum and a module's, whose members' keys the engine
// styles.
func enumSchema() *introspection.Schema {
	schema := &introspection.Schema{Types: introspection.Types{
		{Kind: introspection.TypeKindObject, Name: "Query"},
		{Kind: introspection.TypeKindEnum, Name: "NetworkProtocol", EnumValues: []introspection.EnumValue{{Name: "TCP"}, {Name: "UDP"}}},
		{Kind: introspection.TypeKindEnum, Name: "AppMode", Directives: introspection.Directives{newSourceMapDirective("app")}, EnumValues: []introspection.EnumValue{{Name: "http_only"}, {Name: "grpc"}}},
	}}
	schema.QueryType.Name = "Query"
	generator.SetSchemaParents(schema)
	return schema
}

// The keys of every enum the bindings render, and the alias of every module
// client, are styled in one query, at the schema's version; the core's are
// left out when the core file ships as it is.
func TestGenerate_StylesEnumKeysThroughTheEngine(t *testing.T) {
	engine := &namingtest.Engine{Pascal: map[string]string{
		"TCP": "TCP", "UDP": "UDP", "http_only": "HTTPOnly", "grpc": "GRPC", "app": "App",
	}}
	state, err := generate(context.Background(), generator.Config{
		Engine:       engine,
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}, ClientGenFile, enumSchema(), "v1.0.0-beta.20")
	require.NoError(t, err)
	require.Equal(t, 1, engine.Queries())
	require.Equal(t, []any{"v1.0.0-beta.20"}, engine.Versions())

	core := readOverlay(t, state, "client.gen.ts")
	require.Contains(t, core, `TCP = "TCP",`)
	require.Contains(t, core, `case NetworkProtocol.UDP:`)
	app := readOverlay(t, state, "clients/app.gen.ts")
	require.Contains(t, app, `HTTPOnly = "http_only",`)
	require.Contains(t, app, `GRPC = "grpc",`)

	// With the library's core shipped as it is, only the module's are styled.
	coreFile := filepath.Join(t.TempDir(), "client.gen.ts")
	require.NoError(t, os.WriteFile(coreFile, []byte("// the library's core\n"), 0o600))
	engine = &namingtest.Engine{Pascal: map[string]string{"http_only": "HTTPOnly", "grpc": "GRPC", "app": "App"}}
	_, err = generate(context.Background(), generator.Config{
		Engine:       engine,
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, CoreFile: coreFile},
	}, ClientGenFile, enumSchema(), "v1.0.0-beta.20")
	require.NoError(t, err)
	require.Equal(t, 1, engine.Queries())
}

// A name the engine can't style fails generation, and without an engine
// nothing is styled.
func TestGenerate_FailsOnAnUnstyledName(t *testing.T) {
	_, err := generate(context.Background(), generator.Config{
		Engine:       &namingtest.Engine{Pascal: map[string]string{"TCP": "TCP"}},
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app"},
	}, ClientGenFile, enumSchema(), "v1.0.0-beta.20")
	require.ErrorContains(t, err, `fake engine: can't style`)

	_, err = generate(context.Background(), generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app"},
	}, ClientGenFile, enumSchema(), "v1.0.0-beta.20")
	require.ErrorContains(t, err, `identifiers: no engine to style`)
}

// A name looked up that wasn't styled up front is styled directly.
func TestIdentifiers_StylesAMissDirectly(t *testing.T) {
	engine := &namingtest.Engine{Pascal: map[string]string{"my-dep": "MyDep"}}
	ids, err := templates.LoadIdentifiers(context.Background(), engine, "", nil)
	require.NoError(t, err)
	require.Equal(t, 0, engine.Queries())

	got, err := ids.Pascal("my-dep")
	require.NoError(t, err)
	require.Equal(t, "MyDep", got)
	got, err = ids.Pascal("my-dep")
	require.NoError(t, err)
	require.Equal(t, "MyDep", got)
	require.Equal(t, 1, engine.Queries())
	require.Equal(t, []any{nil}, engine.Versions(), "no schema version leaves the dictionary to the engine")
}
