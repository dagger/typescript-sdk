package templates

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestToPascalCase guards the acronym handling that strcase.ToCamel gets wrong:
// enum converter functions are defined via pascalCase but called by their raw
// schema name, so for acronym-leading names (LLM*) the two must agree or the
// generated client fails to type-check (TS2552).
func TestToPascalCase(t *testing.T) {
	cases := map[string]string{
		"LLMContentBlockKind": "LLMContentBlockKind",
		"LLMMessageRole":      "LLMMessageRole",
		"llm":                 "LLM",
		"NetworkProtocol":     "NetworkProtocol",
		"TypeDefKind":         "TypeDefKind",
		"CacheSharingMode":    "CacheSharingMode",
	}
	for in, want := range cases {
		if got := toPascalCase(in); got != want {
			t.Errorf("toPascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFormatMemberName covers the one case it exists for: a module whose name
// starts with an underscore gets a capitalised schema field, and its binding
// would otherwise be the only member of the API not shaped like the rest.
func TestFormatMemberName(t *testing.T) {
	funcs := typescriptTemplateFuncs{}

	require.Equal(t, "testSdkMaxDev", funcs.formatMemberName("TestSdkMaxDev"))
	// Already conventional: unchanged.
	require.Equal(t, "helloWorld", funcs.formatMemberName("helloWorld"))
	require.Equal(t, "gendep", funcs.formatMemberName("gendep"))
	// Keyword handling still applies, after lowering.
	require.Equal(t, "function_", funcs.formatMemberName("Function"))
	require.Equal(t, "", funcs.formatMemberName(""))
}
