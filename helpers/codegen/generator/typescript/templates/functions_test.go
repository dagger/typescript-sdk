package templates

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

// TestFromOutputDir covers the case the goldens do not: a module whose code
// imports from outside its own directory (the `include` feature) records a
// source path that already climbs, and the dispatcher's own depth has to be
// added to it rather than replace it.
func TestFromOutputDir(t *testing.T) {
	require.Equal(t, "./clients/loader.gen.js", fromOutputDir("", "clients/loader.gen.js"))
	require.Equal(t, "./clients/loader.gen.js", fromOutputDir(".", "clients/loader.gen.js"))
	require.Equal(t, "../src/index", fromOutputDir("entrypoint", "src/index"))
	require.Equal(t, "../src/index", fromOutputDir("entrypoint", "./src/index"))
	require.Equal(t, "../../shared/lib", fromOutputDir("entrypoint", "../shared/lib"))
	require.Equal(t, "../../src/index", fromOutputDir("a/b", "src/index"))
}
