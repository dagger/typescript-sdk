package templates

import "testing"

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
