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
