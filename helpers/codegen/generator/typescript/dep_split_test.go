package typescriptgenerator

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/typescript/templates"
	"codegen/introspection"
)

// TestClientTemplate_RendersModuleClient renders the per-module client template
// against a small hand-crafted schema and asserts the unified shape:
//   - module-owned scalar / class are emitted;
//   - the fields the module contributes to Query become the file's own Client
//     class, with a dag instance and a mirroring top-level function;
//   - nothing declaration-merges into the core file, and no augmentation
//     function is emitted for the core file to call.
func TestClientTemplate_RendersModuleClient(t *testing.T) {
	helloModule := newSourceMapDirective("hello")

	full := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			// Extendable type with one module-contributed field.
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					{
						Name: "hello",
						TypeRef: &introspection.TypeRef{
							Kind: introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{
								Kind: introspection.TypeKindObject,
								Name: "Hello",
							},
						},
						Directives: introspection.Directives{helloModule},
					},
				},
			},
			// Module-owned scalar.
			{
				Kind:        introspection.TypeKindScalar,
				Name:        "HelloID",
				Description: "Hello identifier.",
				Directives:  introspection.Directives{helloModule},
			},
			// Module-owned regular class, referencing a core class.
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{helloModule},
				Fields: []*introspection.Field{
					{
						Name: "ctr",
						TypeRef: &introspection.TypeRef{
							Kind: introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{
								Kind: introspection.TypeKindObject,
								Name: "Container",
							},
						},
					},
				},
			},
			// Core type the module references.
			{
				Kind: introspection.TypeKindObject,
				Name: "Container",
				Fields: []*introspection.Field{
					{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}},
				},
			},
		},
	}
	generator.SetSchemaParents(full)

	depSchema := full.Include("hello")
	generator.SetSchemaParents(depSchema)

	tmpl := templates.New("v0.21.0", full, "", generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"},
	})

	out := renderModuleClientTemplate(t, tmpl, depSchema, "hello")

	// Module-owned scalar and class must appear.
	require.Contains(t, out, "HelloID",
		"module-owned scalar must be emitted in the module file")
	require.Contains(t, out, "export class Hello extends BaseClient",
		"module-owned class must be emitted in the module file")

	// The contributed Query fields become the file's own Client, with a dag
	// and a mirroring top-level function.
	require.Contains(t, out, "export class Client extends BaseClient",
		"the module's contributed root fields must become its own Client class")
	require.Contains(t, out, "hello = (", "the root field must be a Client method")
	require.Contains(t, out, "export const dag = new Client()",
		"the module client must carry its own dag")
	require.Contains(t, out, "export function hello(): Hello {",
		"each root field must be mirrored as a top-level function")
	require.Contains(t, out, "return dag.hello()")

	// Nothing merges into the core file anymore.
	require.NotContains(t, out, "declare module",
		"the module file must not declaration-merge into the core file")
	require.NotContains(t, out, ".prototype.",
		"the module file must not patch prototypes")
	require.NotContains(t, out, "Augmentations",
		"no augmentation function may be emitted")

	// A module client sits under clients/, apart from the library, so it reaches
	// the runtime and core through the @dagger.io/dagger package specifier.
	require.Regexp(t, `import\s*\{\s*Context,\s*BaseClient\s*\}\s*from "@dagger\.io/dagger"`, out,
		"BaseClient must be imported alongside Context from the package")

	// The referenced core class is value-imported from the package (bodies
	// construct it), and the value import is not a type-only one.
	require.Regexp(t, `import \{[^}]*\bContainer\b[^}]*\} from "@dagger\.io/dagger"`, out)
	require.NotRegexp(t, `import type \{[^}]*\bContainer\b`, out)
}

// TestClientTemplate_ImportsSiblingModuleTypes asserts that a module whose API
// references a type owned by another module imports it from that module's own
// generated file. This replaces the old fail-closed sibling guard: with every
// module in its own client file, a sibling type has an owning file to import
// from — the case a module's own API returning a dependency's type hits on
// every self-client generation.
func TestClientTemplate_ImportsSiblingModuleTypes(t *testing.T) {
	helloModule := newSourceMapDirective("hello")
	otherModule := newSourceMapDirective("other")

	full := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			newType("Query", introspection.TypeKindObject, nil),
			// hello's own class, returning a class owned by `other`.
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{helloModule},
				Fields: []*introspection.Field{
					{
						Name: "borrowed",
						TypeRef: &introspection.TypeRef{
							Kind: introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{
								Kind: introspection.TypeKindObject,
								Name: "Other",
							},
						},
					},
				},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Other",
				Directives: introspection.Directives{otherModule},
				Fields: []*introspection.Field{
					{Name: "value", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}}},
				},
			},
		},
	}
	generator.SetSchemaParents(full)

	depSchema := full.Include("hello")
	generator.SetSchemaParents(depSchema)

	tmpl := templates.New("v0.21.0", full, "", generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"},
	})

	out := renderModuleClientTemplate(t, tmpl, depSchema, "hello")

	require.Regexp(t, `import \{[^}]*\bOther\b[^}]*\} from "@dagger\.io/other"`, out,
		"a sibling-owned class must be value-imported from the sibling's package")
	require.Contains(t, out, "return new Other(ctx)",
		"the body must construct the imported sibling class")
}

// TestClientTemplate_ImportsRootArgTypes locks that a core type referenced only
// as an argument to a module's root field — its constructor's `ws: Workspace`,
// never constructed or returned — is still imported. The field becomes a method
// on the file's own Client, so its argument types have to be resolvable.
func TestClientTemplate_ImportsRootArgTypes(t *testing.T) {
	helloModule := newSourceMapDirective("hello")

	full := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					{
						Name:       "hello",
						TypeRef:    &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Hello"}},
						Directives: introspection.Directives{helloModule},
						// A core object used only as a required argument, encoded
						// the way the engine emits it: a raw ID scalar carrying an
						// @expectedType directive naming the object.
						Args: introspection.InputValues{
							{
								Name:       "ws",
								TypeRef:    &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ID"}},
								Directives: introspection.Directives{expectedTypeDirective("Workspace")},
							},
						},
					},
				},
			},
			{Kind: introspection.TypeKindObject, Name: "Hello", Directives: introspection.Directives{helloModule}, Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "HelloID"}}}},
			{Kind: introspection.TypeKindScalar, Name: "HelloID", Directives: introspection.Directives{helloModule}},
			{Kind: introspection.TypeKindObject, Name: "Workspace", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "WorkspaceID"}}}},
		},
	}
	generator.SetSchemaParents(full)
	depSchema := full.Include("hello")
	generator.SetSchemaParents(depSchema)

	tmpl := templates.New("v0.21.0", full, "", generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"},
	})

	out := renderModuleClientTemplate(t, tmpl, depSchema, "hello")

	require.Contains(t, out, "hello = (ws: Workspace", "the root field's arg must render")
	require.Regexp(t, `import \{[^}]*\bWorkspace\b[^}]*\} from "@dagger\.io/dagger"`, out,
		"a core type used only as a root-field argument must still be imported")
}

// TestClientTemplate_ServesModuleOnUse asserts a module client whose Bound
// metadata is set serves its own module before the first query, through one
// serveModule call for both kinds: a git module by canonical ref + pin, a local
// one by workspace-root-absolute path, and its dag carries the serve.
func TestClientTemplate_ServesModuleOnUse(t *testing.T) {
	buildSchema := func() *introspection.Schema {
		helloModule := newSourceMapDirective("hello")
		schema := &introspection.Schema{
			QueryType: struct {
				Name string `json:"name,omitempty"`
			}{Name: "Query"},
			Types: introspection.Types{
				{
					Kind: introspection.TypeKindObject,
					Name: "Query",
					Fields: []*introspection.Field{
						{Name: "hello", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Hello"}}, Directives: introspection.Directives{helloModule}},
					},
				},
				{Kind: introspection.TypeKindObject, Name: "Hello", Directives: introspection.Directives{helloModule}, Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "HelloID"}}}},
				{Kind: introspection.TypeKindScalar, Name: "HelloID", Directives: introspection.Directives{helloModule}},
			},
		}
		generator.SetSchemaParents(schema)
		return schema
	}
	tmpl := func() *template.Template {
		return templates.New("v0.21.0", buildSchema(), "", generator.Config{ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"}})
	}

	t.Run("git module serves by ref and pin", func(t *testing.T) {
		out := renderModuleClientTemplateBound(t, tmpl(), buildSchema().Include("hello"), "hello",
			&generator.BoundModule{Name: "hello", Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abc"})
		require.Contains(t, out, `import { dag as __dag } from "@dagger.io/dagger"`)
		require.Contains(t, out, `await __dag.serveModule("github.com/foo/hello@main", { refPin: "abc" })`)
		require.Contains(t, out, `new Context().withServe({ key: "hello", run: __serveModule })`)
	})

	t.Run("local module serves by workspace path", func(t *testing.T) {
		out := renderModuleClientTemplateBound(t, tmpl(), buildSchema().Include("hello"), "hello",
			&generator.BoundModule{Name: "hello", Kind: "DIR_SOURCE", Path: ".dagger/modules/hello"})
		require.Contains(t, out, `await __dag.serveModule("/.dagger/modules/hello")`)
		require.Contains(t, out, "new Context().withServe({")
		require.NotContains(t, out, "refPin",
			"a workspace path carries no version to pin")
	})

	t.Run("neither kind reaches for a raw query", func(t *testing.T) {
		for _, bound := range []*generator.BoundModule{
			{Name: "hello", Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abc"},
			{Name: "hello", Kind: "DIR_SOURCE", Path: ".dagger/modules/hello"},
		} {
			out := renderModuleClientTemplateBound(t, tmpl(), buildSchema().Include("hello"), "hello", bound)
			require.NotContains(t, out, "getGQLClient().request",
				"serveModule resolves the address engine-side, so no client hand-assembles a document")
			require.NotContains(t, out, "currentWorkspace")
		}
	})

	t.Run("no bound metadata means no serve hook", func(t *testing.T) {
		out := renderModuleClientTemplate(t, tmpl(), buildSchema().Include("hello"), "hello")
		require.NotContains(t, out, "__serveModule")
		require.NotContains(t, out, "withServe")
		require.Contains(t, out, "export const dag = new Client()")
	})
}

// TestHeaderTemplate_KeepsCoreOnly renders the header template against a schema
// containing two modules and asserts the core file no longer imports,
// re-exports, or wires up anything for them: the merged namespace is gone.
func TestHeaderTemplate_KeepsCoreOnly(t *testing.T) {
	full := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			newType("Hello", introspection.TypeKindObject,
				introspection.Directives{newSourceMapDirective("hello")}),
			newType("MyDep", introspection.TypeKindObject,
				introspection.Directives{newSourceMapDirective("myDep")}),
		},
	}

	tmpl := templates.New("v0.21.0", full, "", generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"},
	})

	var b bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&b, "header", nil))
	out := b.String()

	require.Contains(t, out, "export { BaseClient }",
		"client.gen.ts must keep re-exporting BaseClient for existing consumers")
	require.NotContains(t, out, "export *",
		"the core file must not re-export module files")
	require.NotContains(t, out, "hello.gen.js",
		"the core file must not import module files")
	require.NotContains(t, out, "my-dep.gen.js")
	require.NotContains(t, out, "Augmentations")
}

// TestGenerate_SplitsDependencyFiles exercises the full generate() flow and
// asserts the core file excludes the module and its file is a self-contained
// client.
func TestGenerate_SplitsDependencyFiles(t *testing.T) {
	helloModule := newSourceMapDirective("hello")

	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					{
						Name: "hello",
						TypeRef: &introspection.TypeRef{
							Kind: introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{
								Kind: introspection.TypeKindObject,
								Name: "Hello",
							},
						},
						Directives: introspection.Directives{helloModule},
					},
				},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{helloModule},
				Fields: []*introspection.Field{
					{
						Name: "id",
						TypeRef: &introspection.TypeRef{
							Kind: introspection.TypeKindNonNull,
							OfType: &introspection.TypeRef{
								Kind: introspection.TypeKindScalar,
								Name: "HelloID",
							},
						},
					},
				},
			},
			{
				Kind:       introspection.TypeKindScalar,
				Name:       "HelloID",
				Directives: introspection.Directives{helloModule},
			},
		},
	}

	// The real pipeline (codegen.go) sets parents before generating; mirror
	// that here since this test calls generate() directly.
	generator.SetSchemaParents(schema)

	state, err := generate(generator.Config{}, ClientGenFile, schema, "v0.21.0")
	require.NoError(t, err)

	core := readOverlay(t, state, "client.gen.ts")
	dep := readOverlay(t, state, "hello.gen.ts")

	// The core file neither declares the module's class nor knows the module
	// file exists.
	require.NotContains(t, core, "export class Hello extends BaseClient")
	require.NotContains(t, core, "hello.gen.js")
	require.NotContains(t, core, "export *")

	// The module file is a self-contained client.
	require.Contains(t, dep, "export class Hello extends BaseClient")
	require.Contains(t, dep, "export class Client extends BaseClient")
	require.Contains(t, dep, "export const dag = new Client()")
	require.Contains(t, dep, "export function hello(): Hello {")

	// Library mode (no ModuleConfig) has no entrypoint, so no loader.
	_, err = state.Overlay.Open("loader.gen.ts")
	require.Error(t, err, "library generation must not emit a loader")
}

// TestGenerate_KeepsOwnTypesInClient checks that only dependencies are split:
// the module being generated for keeps its own types in client.gen.ts.
func TestGenerate_SplitsOwnTypesLikeADependency(t *testing.T) {
	appModule := newSourceMapDirective("app")
	depModule := newSourceMapDirective("dep")
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
			{Kind: introspection.TypeKindObject, Name: "App", Directives: introspection.Directives{appModule}, Fields: []*introspection.Field{strField("name")}},
			{Kind: introspection.TypeKindObject, Name: "Dep", Directives: introspection.Directives{depModule}, Fields: []*introspection.Field{strField("value")}},
		},
	}
	generator.SetSchemaParents(schema)

	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app"},
	}, ClientGenFile, schema, "v0.21.0")
	require.NoError(t, err)

	core := readOverlay(t, state, "client.gen.ts")
	depFile := readOverlay(t, state, "clients/dep.gen.ts")

	// The module's own type splits out like any other, and the core file holds
	// only core types — no re-exports, no knowledge of the module files.
	require.NotContains(t, core, "export class App extends BaseClient")
	require.NotContains(t, core, "export *")
	require.NotContains(t, core, "app.gen.js")
	require.NotContains(t, core, "dep.gen.js")

	require.Contains(t, depFile, "export class Dep extends BaseClient")
	require.Contains(t, readOverlay(t, state, "clients/app.gen.ts"), "export class App extends BaseClient")
}

// TestGenerate_Module_EmitsLoader checks the loader emitted beside a module's
// bindings: an explicit type-name -> class map covering core and module-owned
// classes, each entry pointing at its owning file's namespace import.
func TestGenerate_Module_EmitsLoader(t *testing.T) {
	depModule := newSourceMapDirective("myDep")

	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{Kind: introspection.TypeKindObject, Name: "Container", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}}}},
			{Kind: introspection.TypeKindObject, Name: "MyDep", Directives: introspection.Directives{depModule}, Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "MyDepID"}}}},
		},
	}
	generator.SetSchemaParents(schema)

	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}, ClientGenFile, schema, "v0.21.0")
	require.NoError(t, err)

	loader := readOverlay(t, state, "clients/loader.gen.ts")
	require.Contains(t, loader, `import * as __core from "@dagger.io/dagger"`)
	require.Contains(t, loader, `import * as __modMyDep from "@dagger.io/my-dep"`)
	require.Contains(t, loader, `"Container": __core.Container,`)
	require.Contains(t, loader, `"MyDep": __modMyDep.MyDep,`)
	require.Contains(t, loader, "export function __loadObject(")
}

// TestGenerate_RejectsReservedModuleNames asserts a module whose kebab-cased
// name would claim a generated core or loader file fails generation instead of
// silently overwriting it.
func TestGenerate_RejectsReservedModuleNames(t *testing.T) {
	buildSchema := func(module string) *introspection.Schema {
		schema := &introspection.Schema{
			QueryType: struct {
				Name string `json:"name,omitempty"`
			}{Name: "Query"},
			Types: introspection.Types{
				newType("Something", introspection.TypeKindObject,
					introspection.Directives{newSourceMapDirective(module)}),
			},
		}
		generator.SetSchemaParents(schema)
		return schema
	}

	// A module scope keeps its clients under clients/, apart from the core
	// client.gen.ts, so "loader" (a sibling of the module files) is the only
	// reserved name; "client" no longer collides.
	_, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}, ClientGenFile, buildSchema("loader"), "v0.21.0")
	require.Error(t, err, `module named "loader" must be rejected in a module scope`)
	require.ErrorContains(t, err, "loader")

	_, err = generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}, ClientGenFile, buildSchema("client"), "v0.21.0")
	require.NoError(t, err, `"client" is a directory apart from the module clients now`)

	// A flat standalone client shares its root with the core client.gen.ts, so
	// "client" is reserved there (and the loader is not emitted, so it isn't).
	_, err = generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{FlatClients: true},
	}, ClientGenFile, buildSchema("client"), "v0.21.0")
	require.Error(t, err, `module named "client" must be rejected in a flat client scope`)
	require.ErrorContains(t, err, "client")
}

// TestGenerate_Client_SplitsBoundModule checks the standalone-client layout
// converges with a module's: a core client.gen.ts backed by the vendored
// library (./core.js), the bound module split into its own flat <module>.gen.ts
// client importing @dagger.io/dagger, and no loader.
func TestGenerate_Client_SplitsBoundModule(t *testing.T) {
	helloModule := newSourceMapDirective("hello")

	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					{
						Name:       "hi",
						TypeRef:    &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}},
						Directives: introspection.Directives{helloModule},
					},
				},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{helloModule},
				Fields:     []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "HelloID"}}}},
			},
			{Kind: introspection.TypeKindScalar, Name: "HelloID", Directives: introspection.Directives{helloModule}},
			// A pure core type stays in the core file.
			{Kind: introspection.TypeKindObject, Name: "Container", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}}}},
		},
	}
	generator.SetSchemaParents(schema)

	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{
			FlatClients:  true,
			BoundModules: []generator.BoundModule{{Name: "hello", Kind: "GIT_SOURCE", Ref: "github.com/foo/hello@main", Pin: "abcdef"}},
		},
	}, ClientGenFile, schema, "v0.21.0")
	require.NoError(t, err)

	// Core file is client.gen.ts, backed by the vendored library, holding only
	// core types.
	core := readOverlay(t, state, "client.gen.ts")
	require.Contains(t, core, `from "./core.js"`)
	require.NotContains(t, core, "export class Hello extends BaseClient",
		"the bound module's type must be split out of the core file")
	require.Contains(t, core, "export class Container extends BaseClient",
		"a pure core type stays in the core file")
	require.NotContains(t, core, "export *")
	require.NotContains(t, core, "hello.gen.js")

	// The bound module lands flat in hello.gen.ts as its own client, serving its
	// module on use and reaching the runtime through the package.
	hello := readOverlay(t, state, "hello.gen.ts")
	require.Contains(t, hello, "export class Hello extends BaseClient")
	require.Contains(t, hello, "export class Client extends BaseClient")
	require.Contains(t, hello, "export const dag = new Client(")
	require.Contains(t, hello, `await __dag.serveModule("github.com/foo/hello@main", { refPin: "abcdef" })`)
	require.Contains(t, hello, "export function hi(): Promise<string> {")
	require.Contains(t, hello, `import { Context, BaseClient } from "@dagger.io/dagger"`)
	require.NotContains(t, hello, "declare module")

	// A flat client emits no dagger.gen.ts and no loader.
	_, err = state.Overlay.Open("dagger.gen.ts")
	require.Error(t, err, "client generation must not emit dagger.gen.ts")
	_, err = state.Overlay.Open("loader.gen.ts")
	require.Error(t, err, "client generation must not emit a loader")
}

// TestClientTemplate_CoreValuesAreValueImported guards the systemic gap where a
// module method returning/accepting a core type emitted `new Container(ctx)` and
// `NetworkProtocolNameToValue(...)` against a type-only import (TS1361 + runtime
// ReferenceError). Core classes the bodies construct and the enum converters
// they call must be *value*-imported; pure signature types stay type-only.
func TestClientTemplate_CoreValuesAreValueImported(t *testing.T) {
	helloModule := newSourceMapDirective("hello")
	obj := func(ref string) *introspection.TypeRef {
		return &introspection.TypeRef{
			Kind:   introspection.TypeKindNonNull,
			OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: ref},
		}
	}
	enumRef := func(name string) *introspection.TypeRef {
		return &introspection.TypeRef{
			Kind:   introspection.TypeKindNonNull,
			OfType: &introspection.TypeRef{Kind: introspection.TypeKindEnum, Name: name},
		}
	}

	full := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind:       introspection.TypeKindObject,
				Name:       "Hello",
				Directives: introspection.Directives{helloModule},
				Fields: []*introspection.Field{
					// Returns a core object -> `new Container(ctx)`.
					{Name: "ctr", TypeRef: obj("Container")},
					// Takes a core object as an arg (signature type only) and
					// returns a core enum -> `NetworkProtocolNameToValue(...)`.
					{
						Name:    "proto",
						TypeRef: enumRef("NetworkProtocol"),
						Args: []introspection.InputValue{
							{Name: "dir", TypeRef: obj("Directory")},
						},
					},
				},
			},
			// Core types referenced above.
			{Kind: introspection.TypeKindObject, Name: "Container", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}}}},
			{Kind: introspection.TypeKindObject, Name: "Directory", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "DirectoryID"}}}},
			{Kind: introspection.TypeKindEnum, Name: "NetworkProtocol", EnumValues: []introspection.EnumValue{{Name: "TCP"}, {Name: "UDP"}}},
		},
	}
	generator.SetSchemaParents(full)
	depSchema := full.Include("hello")
	generator.SetSchemaParents(depSchema)

	tmpl := templates.New("v0.21.0", full, "", generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "host"},
	})

	out := renderModuleClientTemplate(t, tmpl, depSchema, "hello")

	// Core classes the bodies construct are value-imported (not `import type`),
	// and both arg-only (Directory) and constructed (Container) objects appear.
	require.Regexp(t, `import \{[^}]*\bContainer\b[^}]*\} from "@dagger\.io/dagger"`, out,
		"constructed core class must be value-imported")
	require.Regexp(t, `import \{[^}]*\bDirectory\b[^}]*\} from "@dagger\.io/dagger"`, out,
		"core class used as a signature type must still be importable")
	// The enum converter called in the body is value-imported and the body uses it.
	require.Regexp(t, `import \{[^}]*\bNetworkProtocolNameToValue\b[^}]*\} from "@dagger\.io/dagger"`, out,
		"enum converter must be value-imported")
	require.Contains(t, out, "NetworkProtocolNameToValue(", "body must call the imported converter")
	require.Contains(t, out, "return new Container(ctx)", "body must construct the core class")

	// The constructed class must not be pulled in as a type-only import (that
	// would be erased at runtime).
	require.NotRegexp(t, `import type \{[^}]*\bContainer\b`, out,
		"a constructed class must not be type-only imported")
}

func readOverlay(t *testing.T, state *generator.GeneratedState, name string) string {
	t.Helper()
	f, err := state.Overlay.Open(name)
	require.NoError(t, err, "expected generated file %q", name)
	defer f.Close()
	var b bytes.Buffer
	_, err = b.ReadFrom(f)
	require.NoError(t, err)
	return b.String()
}

func renderModuleClientTemplate(t *testing.T, tmpl *template.Template, schema *introspection.Schema, depName string) string {
	return renderModuleClientTemplateBound(t, tmpl, schema, depName, nil)
}

func renderModuleClientTemplateBound(t *testing.T, tmpl *template.Template, schema *introspection.Schema, depName string, bound *generator.BoundModule) string {
	t.Helper()
	data := struct {
		Schema        *introspection.Schema
		SchemaVersion string
		Types         []*introspection.Type
		DepName       string
		Bound         *generator.BoundModule
	}{
		Schema:        schema,
		SchemaVersion: "v0.21.0",
		Types:         schema.Types,
		DepName:       depName,
		Bound:         bound,
	}
	var b bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&b, "module_client", data))
	return b.String()
}

func newType(name string, kind introspection.TypeKind, directives introspection.Directives) *introspection.Type {
	return &introspection.Type{
		Kind:       kind,
		Name:       name,
		Directives: directives,
	}
}

func expectedTypeDirective(typeName string) *introspection.Directive {
	v := `"` + typeName + `"`
	return &introspection.Directive{
		Name: "expectedType",
		Args: []*introspection.DirectiveArg{{Name: "name", Value: &v}},
	}
}

func newSourceMapDirective(moduleName string) *introspection.Directive {
	v := `"` + moduleName + `"`
	return &introspection.Directive{
		Name: "sourceMap",
		Args: []*introspection.DirectiveArg{
			{Name: "module", Value: &v},
		},
	}
}
