package typescriptgenerator

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/introspection"
)

// compatSchema builds a module-facing schema shaped like the case the compat
// shim exists for: one dependency contributing a root field with an optional
// argument and an object return, a second contributing only a scalar root field
// (so it owns no object type and never reaches the loader's map), plus the two
// fields the shim must skip — one named like a TS keyword, one shadowing a core
// Query field.
func compatSchema(t *testing.T) *introspection.Schema {
	t.Helper()

	myDep := newSourceMapDirective("myDep")
	plain := newSourceMapDirective("plain")

	str := &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}}
	obj := func(name string) *introspection.TypeRef {
		return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: name}}
	}

	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{
					// Core's own root field, which "plain" also declares below.
					{Name: "container", TypeRef: obj("Container")},
					{
						Name:       "myDep",
						TypeRef:    obj("MyDep"),
						Directives: introspection.Directives{myDep},
						Args: introspection.InputValues{
							{Name: "name", TypeRef: str},
							{
								Name:    "ctr",
								TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ID"},
								// An object-typed optional argument, encoded the
								// way the engine emits one.
								Directives: introspection.Directives{expectedTypeDirective("Container")},
							},
						},
					},
					// A keyword name: unreachable as a member, so skipped.
					{Name: "import", TypeRef: str, Directives: introspection.Directives{myDep}},
					{Name: "plain", TypeRef: str, Directives: introspection.Directives{plain}},
					// Shadows the core root field above, so skipped.
					{Name: "container", TypeRef: obj("Container"), Directives: introspection.Directives{plain}},
				},
			},
			{
				Kind:       introspection.TypeKindObject,
				Name:       "MyDep",
				Directives: introspection.Directives{myDep},
				Fields:     []*introspection.Field{{Name: "value", TypeRef: str}},
			},
			// "plain" owns a scalar and no object type, so it is split into its
			// own client but contributes nothing to the loader's class map.
			{Kind: introspection.TypeKindScalar, Name: "PlainID", Directives: introspection.Directives{plain}},
			{Kind: introspection.TypeKindObject, Name: "Container", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}}}},
		},
	}
	generator.SetSchemaParents(schema)

	return schema
}

// TestGenerateModule_CompatShim pins the shim for the embedded [runtime]
// layout, where it sits flat in sdk/ beside the clients and reaches them
// through their @dagger.io/<module> specifiers.
func TestGenerateModule_CompatShim(t *testing.T) {
	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, FlatClients: true},
	}, ClientGenFile, compatSchema(t), "v0.21.0")
	require.NoError(t, err)

	got := readOverlay(t, state, "compat.gen.ts")

	const goldenPath = "testdata/compat_flat_want.ts"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

// TestGenerateModule_CompatShimPackaged pins the shim for the entrypoint
// layout, where each client is its own package directory and the shim — like
// the loader beside it — reaches them by relative path rather than by package
// name, because a client the module has not installed still has to resolve at
// dispatch time.
func TestGenerateModule_CompatShimPackaged(t *testing.T) {
	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, PackagedClients: true},
	}, ClientGenFile, compatSchema(t), "v0.21.0")
	require.NoError(t, err)

	got := readOverlay(t, state, "clients/compat.gen.ts")

	const goldenPath = "testdata/compat_packaged_want.ts"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

// TestGenerateModule_CompatShimSkips covers the two fields that must not be
// restored, and the module that owns no object type.
//
// The keyword and the core-shadowing field are skipped for different reasons —
// one cannot be written as a member at all, the other would replace a core
// method on the prototype everything shares — and both stay reachable through
// the module's own client.
func TestGenerateModule_CompatShimSkips(t *testing.T) {
	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, FlatClients: true},
	}, ClientGenFile, compatSchema(t), "v0.21.0")
	require.NoError(t, err)

	compat := readOverlay(t, state, "compat.gen.ts")

	require.Contains(t, compat, `__install("myDep"`)
	require.NotContains(t, compat, `__install("import"`,
		"a root field named like a TS keyword cannot be a member and is skipped")
	require.NotContains(t, compat, `__install("container"`,
		"a root field shadowing a core Query field must not replace the core method")

	// "plain" contributes only a scalar root field, so it owns no object type
	// and never appears in the loader's class map — the shim is what reaches it.
	require.Contains(t, compat, `__install("plain"`)
	require.Contains(t, compat, `import { dag as __dagPlain } from "@dagger.io/plain"`)

	loader := readOverlay(t, state, "loader.gen.ts")
	require.NotContains(t, loader, "__modPlain",
		"the module owning no object type has no loader entry, which is the gap the shim covers")
	require.Contains(t, loader, `import "./compat.gen.js"`,
		"the loader must evaluate the shim before any dispatch")
}

// TestGenerateModule_CompatShimOnlyWithClients covers both ends of when the
// file exists at all: a scope with no module client has nothing to restore, and
// a standalone client scope has no loader to evaluate a shim, so neither gets
// one.
func TestGenerateModule_CompatShimOnlyWithClients(t *testing.T) {
	coreOnly := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{Kind: introspection.TypeKindObject, Name: "Query", Fields: []*introspection.Field{{Name: "container", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: "Container"}}}}},
			{Kind: introspection.TypeKindObject, Name: "Container", Fields: []*introspection.Field{{Name: "id", TypeRef: &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "ContainerID"}}}},
		},
	}
	generator.SetSchemaParents(coreOnly)

	state, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true, FlatClients: true},
	}, ClientGenFile, coreOnly, "v0.21.0")
	require.NoError(t, err)
	_, err = state.Overlay.Open("compat.gen.ts")
	require.Error(t, err, "a scope with no module client should get no shim")
	require.NotContains(t, readOverlay(t, state, "loader.gen.ts"), "compat.gen.js",
		"and the loader should not import one")

	// A standalone client scope: clients, but no entrypoint and so no loader.
	state, err = generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{FlatClients: true},
	}, ClientGenFile, compatSchema(t), "v0.21.0")
	require.NoError(t, err)
	_, err = state.Overlay.Open("compat.gen.ts")
	require.Error(t, err, "nothing in a standalone client scope would evaluate a shim")
}

// TestGenerate_RejectsCompatModuleName asserts a module whose kebab-cased name
// would claim the shim's file fails generation rather than silently overwrite
// it — the same rule "loader" already has.
func TestGenerate_RejectsCompatModuleName(t *testing.T) {
	schema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			newType("Something", introspection.TypeKindObject,
				introspection.Directives{newSourceMapDirective("compat")}),
		},
	}
	generator.SetSchemaParents(schema)

	_, err := generate(generator.Config{
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "app", EmitLoader: true},
	}, ClientGenFile, schema, "v0.21.0")
	require.Error(t, err)
	require.ErrorContains(t, err, "compat")
}

// TestGenerateEntrypoint_LoadsLoaderForSideEffect covers why the entrypoint
// imports the loader twice over.
//
// The named import is not always used: a module with no object-typed argument,
// field or return never calls __loadCoreObject, and a type-stripping loader
// drops an unused named import outright — taking the loader, the compat shim it
// imports, and the restored `dag.<module>()` methods with it. The side-effect
// import is what cannot be dropped.
func TestGenerateEntrypoint_LoadsLoaderForSideEffect(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dispatch bool
	}{
		{name: "legacy entrypoint"},
		{name: "dispatcher", dispatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				EntrypointConfig: &generator.EntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					ModuleRoot:      "/work",
					SDKImportPath:   "@dagger.io/dagger",
					SourceDir:       "src",
					DispatchMode:    tc.dispatch,
					OutputFile:      "out.ts",
				},
			}}

			state, err := gen.GenerateEntrypoint(context.Background())
			require.NoError(t, err)

			require.Contains(t, readOverlay(t, state, "out.ts"), `import "./clients/loader.gen.js"`)
		})
	}
}
