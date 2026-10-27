package typescriptgenerator

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/introspection"
)

// moduleSchema builds a schema shaped like a module's own view: the module's
// type plus one dependency's, both carrying a sourceMap directive so the
// dependency splitting has something to key off.
func moduleSchema(t *testing.T) *introspection.Schema {
	t.Helper()

	strField := func(name string) *introspection.Field {
		return &introspection.Field{
			Name:    name,
			TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}},
		}
	}

	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind:       introspection.TypeKindObject,
				Name:       "App",
				Directives: introspection.Directives{newSourceMapFileDirective("app", "ci/app/src/index.ts", 12)},
				Fields:     []*introspection.Field{strField("name")},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Gendep",
				Directives: introspection.Directives{newSourceMapFileDirective("gendep", "ci/dep/src/index.ts", 6)},
				Fields:     []*introspection.Field{strField("value")},
			},
		},
	}
	generator.SetSchemaParents(schema)

	return schema
}

// TestGenerateModule_Layout covers the on-disk contract module codegen has with
// the engine runtime: the runtime mounts the module's sdk/ directory as
// @dagger.io/dagger and requires sdk/client.gen.ts, so the bindings must land
// flat in the output directory (not nested under sdk/src/api) and import the
// bundled library sitting next to them.
func TestGenerateModule_Layout(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}}

	state, err := gen.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	// The core file is the library: it stays at the root (sdk/) and imports the
	// bundle sitting beside it. The module clients live under clients/.
	core := readOverlay(t, state, "client.gen.ts")
	dep := readOverlay(t, state, "clients/gendep.gen.ts")

	require.Contains(t, core, `from "./core.js"`)
	require.NotContains(t, core, `from "@dagger.io/dagger"`)

	// A module client sits under clients/, apart from the library, so it reaches
	// core through the @dagger.io/dagger package specifier, not a relative path.
	require.Contains(t, dep, `from "@dagger.io/dagger"`)
	require.NotContains(t, dep, `from "./core.js"`)

	// Every module splits into its own self-contained client file, the module
	// being generated for included. The core file no longer imports or
	// re-exports any of them.
	require.NotContains(t, core, "export *")
	require.NotContains(t, core, "gendep.gen.js")
	require.NotContains(t, core, "app.gen.js")
	require.Contains(t, dep, "export class Gendep extends BaseClient")
	require.Contains(t, dep, "export const dag = new Client()")
	require.NotContains(t, core, "export class App extends BaseClient")
	require.Contains(t, readOverlay(t, state, "clients/app.gen.ts"), "export class App extends BaseClient")

	// The loader lives beside the module clients and imports each through its
	// package specifier. (This schema carries no core object types, so the
	// core import is exercised in TestGenerate_Module_EmitsLoader.)
	loader := readOverlay(t, state, "clients/loader.gen.ts")
	require.Contains(t, loader, `import * as __modGendep from "@dagger.io/gendep"`)
	require.Contains(t, loader, `"Gendep": __modGendep.Gendep,`)
	require.Contains(t, loader, `"App": __modApp.App,`)
}

// TestGenerateModule_SourceMapPathEscapesToWorkspaceRoot pins the source-map
// breadcrumb rendered next to generated declarations. The engine records a
// declaration's filename relative to the workspace root — for a dependency as
// much as for the module being generated for — so the link has to climb out of
// the directory the client was written into to resolve.
func TestGenerateModule_SourceMapPathEscapesToWorkspaceRoot(t *testing.T) {
	// The packaged layout: ci/app/clients/<module>/<module>.gen.ts, four hops
	// below the root the filenames are recorded against.
	packaged := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName:      "app",
			ClientsDir:      "ci/app/clients",
			PackagedClients: true,
		},
	}}

	state, err := packaged.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	require.Contains(t, readOverlay(t, state, "clients/app/app.gen.ts"), "// app (../../../../ci/app/src/index.ts:12:0)")
	require.Contains(t, readOverlay(t, state, "clients/gendep/gendep.gen.ts"), "// gendep (../../../../ci/dep/src/index.ts:6:0)")

	// The embedded layout writes the clients flat into the module's sdk/, one
	// level less deep.
	flat := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{
			ModuleName:  "app",
			ClientsDir:  "ci/app/sdk",
			FlatClients: true,
		},
	}}

	state, err = flat.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	require.Contains(t, readOverlay(t, state, "gendep.gen.ts"), "// gendep (../../../ci/dep/src/index.ts:6:0)")
}

// TestGenerateLibrary_RendersTheCoreAModuleShips pins the one property a
// module's core rests on: the library's own bindings and a module's render
// from the same schema are the same text, so the library's file can be shipped
// into a module as it is. Both reach the runtime through ./core.js: the bundle
// beside a module's copy, a re-export beside the library's own.
func TestGenerateLibrary_RendersTheCoreAModuleShips(t *testing.T) {
	schema := moduleSchema(t)

	lib, err := (&TypeScriptGenerator{Config: generator.Config{}}).GenerateLibrary(context.Background(), schema, "v0.21.0")
	require.NoError(t, err)
	mod, err := (&TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}}).GenerateModule(context.Background(), schema, "v0.21.0")
	require.NoError(t, err)

	core := readOverlay(t, lib, "client.gen.ts")
	require.Contains(t, core, `from "./core.js"`)
	require.NotContains(t, core, `from "../session/context.js"`)
	require.Equal(t, core, readOverlay(t, mod, "client.gen.ts"))
}

// TestGenerateModule_ShipsCoreFile covers the path the SDK takes: the core
// file comes from the library, and only the module types around it are
// rendered from the schema.
func TestGenerateModule_ShipsCoreFile(t *testing.T) {
	coreFile := filepath.Join(t.TempDir(), "client.gen.ts")
	require.NoError(t, os.WriteFile(coreFile, []byte("// the library's core\n"), 0o600))

	gen := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, CoreFile: coreFile},
	}}
	state, err := gen.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	require.Equal(t, "// the library's core\n", readOverlay(t, state, "client.gen.ts"))
	require.Contains(t, readOverlay(t, state, "clients/loader.gen.ts"), "export function __loadObject(")
}

// TestGenerateClient_Converges covers that a standalone client renders the same
// shape as a module: a core client.gen.ts backed by the vendored library, and
// flat per-module clients reaching the runtime through the package.
func TestGenerateClient_Converges(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{
			FlatClients:  true,
			BoundModules: []generator.BoundModule{{Name: "gendep", Kind: generator.ModuleKindDir, Path: ".dagger/modules/gendep"}},
		},
	}}

	state, err := gen.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	// Core file is client.gen.ts, backed by the vendored library.
	core := readOverlay(t, state, "client.gen.ts")
	require.Contains(t, core, `from "./core.js"`)

	// The clients are flat and reach the runtime through the package, same as a
	// module's clients/ files.
	dep := readOverlay(t, state, "gendep.gen.ts")
	require.Contains(t, dep, `from "@dagger.io/dagger"`)
	require.Contains(t, dep, "export const dag = new Client(")

	require.Contains(t, readOverlay(t, state, "app.gen.ts"), "export class App extends BaseClient")
	_, err = state.Overlay.Open("loader.gen.ts")
	require.Error(t, err)
}

func newSourceMapFileDirective(moduleName, filename string, line int) *introspection.Directive {
	d := newSourceMapDirective(moduleName)
	name := `"` + filename + `"`
	num := strconv.Itoa(line)
	d.Args = append(d.Args,
		&introspection.DirectiveArg{Name: "filename", Value: &name},
		&introspection.DirectiveArg{Name: "line", Value: &num},
	)
	return d
}
