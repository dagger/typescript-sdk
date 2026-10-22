package templates

import (
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/introspection"
)

// identifierFuncs returns template funcs for a schema whose names are
// formatted by the engine's naming rules: one with Query.formatIdentifiers.
func identifierFuncs(t *testing.T, names ...string) typescriptTemplateFuncs {
	t.Helper()
	schema := &introspection.Schema{FormattedNames: map[introspection.NameFormat]map[string]string{}}
	for _, f := range NameFormats {
		formatted, err := introspection.FormatNames(names, f, "v1.0.0-beta.21")
		require.NoError(t, err)
		schema.FormattedNames[f] = formatted
	}
	return typescriptTemplateFuncs{fullSchema: schema}
}

func TestIdentifierNames(t *testing.T) {
	funcs := identifierFuncs(t,
		"shortSha", "parentShas", "withGPU", "http", "sshfsVolume", "llm",
		"pushUrl", "callId", "default", "function", "Foo",
		"TCP", "OBJECT_KIND", "GITHUB_REPO", "PerSession", "DIRECTORY_TYPE",
		"LLMContentBlockKind", "JSONValue", "_internal",
	)

	// methods: CAMEL / UPPERCASE, escaped
	require.Equal(t, "shortSHA", funcs.formatMethodName("shortSha"))
	require.Equal(t, "parentSHAs", funcs.formatMethodName("parentShas"))
	require.Equal(t, "withGPU", funcs.formatMethodName("withGPU"))
	require.Equal(t, "function_", funcs.formatMethodName("function"))
	require.Equal(t, "shortSha", funcs.legacyMethodName("shortSha"))
	require.Equal(t, "shortSHA", funcs.formatMemberName("shortSha"))
	require.Equal(t, "foo", funcs.formatMemberName("Foo"))

	// args: CAMEL / UPPERCASE; escaped only as parameters
	require.Equal(t, "pushURL", funcs.argName("pushUrl"))
	require.Equal(t, "callID", funcs.formatArgName("callId"))
	require.Equal(t, "default", funcs.argName("default"))
	require.Equal(t, "default_", funcs.formatArgName("default"))

	// a leading underscore is never dropped
	require.Equal(t, "_internal", funcs.formatMethodName("_internal"))

	// Opts types and enum converters: PASCAL / UPPERCASE
	require.Equal(t, "ClientHTTPOpts", funcs.optsTypeName("Client", "http"))
	require.Equal(t, "ClientHttpOpts", funcs.legacyOptsTypeName("Client", "http"))
	require.Equal(t, "ClientSSHFSVolumeOpts", funcs.optsTypeName("Client", "sshfsVolume"))
	require.Equal(t, "ClientLLMOpts", funcs.optsTypeName("Client", "llm"))
	require.Equal(t, "LLMContentBlockKind", funcs.pascalCase("LLMContentBlockKind"))
	require.Equal(t, "JSONValue", funcs.pascalCase("JSONValue"))

	// enum members: PASCAL / CAPITALIZED, like the legacy converter
	require.Equal(t, "Tcp", funcs.formatEnum("TCP"))
	require.Equal(t, "ObjectKind", funcs.formatEnum("OBJECT_KIND"))
	require.Equal(t, "GitHubRepo", funcs.formatEnum("GITHUB_REPO"))
	require.Equal(t, "PerSession", funcs.formatEnum("PerSession"))
	require.Equal(t, "DirectoryType", funcs.formatEnum("DIRECTORY_TYPE"))

	// renamed enum members keep their old name as an alias, unless another
	// member has it
	enum := &introspection.Type{Kind: introspection.TypeKindEnum, EnumValues: []introspection.EnumValue{{Name: "GITHUB_REPO"}, {Name: "TCP"}}}
	require.Equal(t, "GithubRepo", funcs.legacyEnumAlias(enum, "GITHUB_REPO"))
	require.Equal(t, "", funcs.legacyEnumAlias(enum, "TCP"))

	// only renamed args are mapped back to their schema name
	renamed := funcs.renamedArgs(introspection.InputValues{
		{Name: "pushUrl"}, {Name: "default"}, {Name: "notInSchema"},
	})
	require.Len(t, renamed, 1)
	require.Equal(t, "pushUrl", renamed[0].Name)

	// method aliases
	parent := &introspection.Type{Name: "GitCommit"}
	alias := funcs.methodAliasFor(&introspection.Field{Name: "shortSha", ParentObject: parent}, false)
	require.Equal(t, &methodAlias{Class: "GitCommit", Name: "shortSHA", Legacy: "shortSha"}, alias)
	require.Nil(t, funcs.methodAliasFor(&introspection.Field{Name: "withGPU", ParentObject: parent}, false))
	require.Nil(t, funcs.methodAliasFor(&introspection.Field{Name: "Foo", ParentObject: parent}, true))
}

// Without engine-formatted names, every name goes through the legacy
// converters.
func TestIdentifierNamesFallback(t *testing.T) {
	for _, funcs := range []typescriptTemplateFuncs{
		{},
		{fullSchema: &introspection.Schema{}},
		identifierFuncs(t, "somethingElse"),
	} {
		require.Equal(t, "shortSha", funcs.formatMethodName("shortSha"))
		require.Equal(t, "function_", funcs.formatMethodName("function"))
		require.Equal(t, "foo", funcs.formatMemberName("Foo"))
		require.Equal(t, "pushUrl", funcs.argName("pushUrl"))
		require.Equal(t, "default_", funcs.formatArgName("default"))
		require.Equal(t, "ClientHttpOpts", funcs.optsTypeName("Client", "http"))
		require.Equal(t, "ClientLLMOpts", funcs.optsTypeName("Client", "llm"))
		require.Equal(t, "LLMContentBlockKind", funcs.pascalCase("LLMContentBlockKind"))
		require.Equal(t, "Tcp", funcs.formatEnum("TCP"))
		require.Equal(t, "GithubRepo", funcs.formatEnum("GITHUB_REPO"))
		require.Empty(t, funcs.renamedArgs(introspection.InputValues{{Name: "pushUrl"}}))
		require.Nil(t, funcs.methodAliasFor(&introspection.Field{Name: "shortSha", ParentObject: &introspection.Type{Name: "GitCommit"}}, false))
	}
}
