package main

import (
	"strings"
	"testing"

	"codegen/generator"
	"codegen/introspection"

	"github.com/stretchr/testify/require"
)

func TestValidateBoundModuleKind(t *testing.T) {
	tests := []struct {
		name    string
		mod     generator.BoundModule
		wantErr bool
	}{
		{name: "git", mod: generator.BoundModule{Kind: "GIT_SOURCE", Ref: "github.com/foo/bar@main", Pin: "abc"}},
		{name: "local", mod: generator.BoundModule{Kind: "LOCAL_SOURCE", Path: "/mods/bar"}},
		{name: "dir (local module resolves as dir)", mod: generator.BoundModule{Kind: "DIR_SOURCE", Path: "/mods/bar"}},
		{name: "unknown rejected", mod: generator.BoundModule{Kind: "WAT"}, wantErr: true},
		{name: "empty rejected", mod: generator.BoundModule{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBoundModuleKind(tt.mod)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestMergeSchemas covers the fold that lets one package serve several modules.
// Each target's schema is core plus that one module, so the core API repeats
// across all of them while each contributes its own types and its own fields on
// Query. Losing a contributed field drops a module's entry point; duplicating a
// core type emits it twice into the same file.
func TestMergeSchemas(t *testing.T) {
	schema := func(module string) *introspection.Schema {
		return &introspection.Schema{
			QueryType: struct {
				Name string `json:"name,omitempty"`
			}{Name: "Query"},
			Types: introspection.Types{
				{
					Kind: introspection.TypeKindObject,
					Name: "Query",
					Fields: []*introspection.Field{
						{Name: "container"},
						{Name: module},
					},
				},
				{Kind: introspection.TypeKindObject, Name: "Container"},
				{Kind: introspection.TypeKindObject, Name: strings.ToUpper(module[:1]) + module[1:]},
			},
		}
	}

	merged := mergeSchemas([]*introspection.Schema{schema("hello"), schema("payments")})

	names := []string{}
	for _, typ := range merged.Types {
		names = append(names, typ.Name)
	}
	require.Equal(t, []string{"Query", "Container", "Hello", "Payments"}, names,
		"each module's types should be added once, and core types kept once")

	fields := []string{}
	for _, field := range merged.Types.Get("Query").Fields {
		fields = append(fields, field.Name)
	}
	require.Equal(t, []string{"container", "hello", "payments"}, fields,
		"every module's Query entry point should survive the merge")
}

// TestFoldClientsIntoModuleSchema covers the half of a scope's clients that goes
// into the module's own bindings. A client schema is core plus one module, and
// its core half hides nothing — so folding it in wholesale would give a module
// bindings for core types its own schema deliberately withholds.
func TestFoldClientsIntoModuleSchema(t *testing.T) {
	dir := t.TempDir()

	moduleSchema := &introspection.Schema{
		QueryType: struct {
			Name string `json:"name,omitempty"`
		}{Name: "Query"},
		Types: introspection.Types{
			{Kind: introspection.TypeKindObject, Name: "Query", Fields: []*introspection.Field{{Name: "container"}}},
			{Kind: introspection.TypeKindObject, Name: "Container"},
		},
	}

	// The source-map directive is what marks a type as a module's, and so what
	// Include filters on.
	const hello = `[{"name":"sourceMap","args":[{"name":"module","value":"\"hello\""}]}]`
	clientSchema := `{"__schema":{"queryType":{"name":"Query"},"types":[
		{"kind":"OBJECT","name":"Query","fields":[
			{"name":"container","type":{"kind":"OBJECT","name":"Container"}},
			{"name":"hello","type":{"kind":"OBJECT","name":"Hello"},"directives":` + hello + `}
		]},
		{"kind":"OBJECT","name":"Container"},
		{"kind":"OBJECT","name":"Secret"},
		{"kind":"OBJECT","name":"Hello","directives":` + hello + `}
	]}}`

	merged, err := foldClientsIntoModuleSchema(moduleSchema, []clientMetaModule{{
		BoundModule: generator.BoundModule{Name: "hello"},
		SchemaPath:  writeFile(t, dir, "hello.json", clientSchema),
	}})
	require.NoError(t, err)

	names := []string{}
	for _, typ := range merged.Types {
		names = append(names, typ.Name)
	}
	require.Contains(t, names, "Hello", "the client target's own type should reach the module's bindings")
	require.NotContains(t, names, "Secret",
		"a core type the module-facing schema withholds must not arrive through a client schema")

	fields := []string{}
	for _, field := range merged.Types.Get("Query").Fields {
		fields = append(fields, field.Name)
	}
	require.Equal(t, []string{"container", "hello"}, fields,
		"the target's entry point should be added once, beside the module's own core fields")
}
