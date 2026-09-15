package typescriptgenerator

import (
	"context"
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
				Directives: introspection.Directives{newSourceMapFileDirective("app", "src/index.ts", 12)},
				Fields:     []*introspection.Field{strField("name")},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Gendep",
				Directives: introspection.Directives{newSourceMapDirective("gendep")},
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
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app"},
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
	require.Contains(t, dep, "export const dag = new Client(__servedContext())")
	require.NotContains(t, core, "export class App extends BaseClient")
	require.Contains(t, readOverlay(t, state, "clients/app.gen.ts"), "export class App extends BaseClient")

	// The loader lives beside the module clients and imports each through its
	// package specifier. (This schema carries no core object types, so the
	// core import is exercised in TestGenerate_Module_EmitsLoader.)
	loader := readOverlay(t, state, "clients/loader.gen.ts")
	require.Contains(t, loader, `import * as __modGendep from "@dagger.io/gendep"`)
	require.Contains(t, loader, `"Gendep": { cls: __modGendep.Gendep,`)
	require.Contains(t, loader, `"App": { cls: __modApp.App,`)
}

// TestGenerateModule_SourceMapPathIsRelativeToSDKDir pins the source-map
// breadcrumb rendered next to generated declarations. The link is recorded
// relative to the module root, while the file quoting it lives one level down
// in sdk/, so it needs one hop up to stay clickable.
func TestGenerateModule_SourceMapPathIsRelativeToSDKDir(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app"},
	}}

	state, err := gen.GenerateModule(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	require.Contains(t, readOverlay(t, state, "clients/app.gen.ts"), "// app (../src/index.ts:12:0)")
}

// TestGenerateLibrary_ImportsRuntimeFromSource covers the third import arm: the
// SDK library's own bindings ship inside the library, so they reach the runtime
// by relative source path rather than through the bundle or the package name.
func TestGenerateLibrary_ImportsRuntimeFromSource(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{}}

	state, err := gen.GenerateLibrary(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	core := readOverlay(t, state, "client.gen.ts")
	require.Contains(t, core, `from "../common/context.js"`)
	require.NotContains(t, core, `from "./core.js"`)
	require.NotContains(t, core, `from "@dagger.io/dagger"`)
}

// TestGenerateClient_ImportsPackage covers the remaining arm: a standalone
// client resolves the SDK through the npm package it depends on.
func TestGenerateClient_ImportsPackage(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		ClientConfig: &generator.ClientGeneratorConfig{
			ModuleName:   "app",
			BoundModules: []generator.BoundModule{{Name: "app", Kind: generator.ModuleKindDir, Path: ".dagger/modules/app"}},
		},
	}}

	state, err := gen.GenerateClient(context.Background(), moduleSchema(t), "v0.21.0")
	require.NoError(t, err)

	core := readOverlay(t, state, "dagger.gen.ts")
	require.Contains(t, core, `from "@dagger.io/dagger"`)
	require.NotContains(t, core, `from "./core.js"`)
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
