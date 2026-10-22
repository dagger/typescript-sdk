package introspection

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func namesTestSchema(withFormatIdentifiers bool) *Schema {
	queryFields := []*Field{{Name: "httpClient", Args: InputValues{{Name: "insecureSkipTLSVerify"}}}}
	if withFormatIdentifiers {
		queryFields = append(queryFields, &Field{Name: "formatIdentifiers", Args: InputValues{{Name: "names"}}})
	}
	schema := &Schema{
		Types: Types{
			{Kind: TypeKindObject, Name: "Query", Fields: queryFields},
			{
				Kind:        TypeKindInputObject,
				Name:        "LLMContentBlockInput",
				InputFields: []InputValue{{Name: "commitSHA"}, {Name: "_"}},
			},
			{
				Kind:       TypeKindEnum,
				Name:       "CacheSharingMode",
				EnumValues: []EnumValue{{Name: "SHARED"}, {Name: "__hidden"}},
			},
			{Kind: TypeKindObject, Name: "__Type", Fields: []*Field{{Name: "ofType"}}},
		},
	}
	schema.QueryType.Name = "Query"
	return schema
}

func TestSchemaNames(t *testing.T) {
	require.Equal(t, []string{
		"CacheSharingMode", "LLMContentBlockInput", "Query", "SHARED",
		"commitSHA", "formatIdentifiers", "httpClient", "insecureSkipTLSVerify", "names",
	}, namesTestSchema(true).Names())
}

func TestHasFormatIdentifiers(t *testing.T) {
	require.True(t, namesTestSchema(true).HasFormatIdentifiers())
	require.False(t, namesTestSchema(false).HasFormatIdentifiers())
	require.False(t, (&Schema{}).HasFormatIdentifiers())
	require.False(t, (*Schema)(nil).HasFormatIdentifiers())
}

var (
	camelUpper  = NameFormat{Casing: CasingCamel, Acronyms: AcronymsUppercase}
	pascalUpper = NameFormat{Casing: CasingPascal, Acronyms: AcronymsUppercase}
	pascalCap   = NameFormat{Casing: CasingPascal, Acronyms: AcronymsCapitalized}
)

// The gate: a schema without Query.formatIdentifiers (a view before
// v1.0.0-beta.21) formats nothing, so codegen keeps its legacy converters.
func TestLoadFormattedNamesGate(t *testing.T) {
	schema := namesTestSchema(false)
	require.NoError(t, schema.LoadFormattedNames("v1.0.0-beta.20", camelUpper))
	require.Nil(t, schema.FormattedNames)
	_, ok := schema.FormattedName("httpClient", camelUpper)
	require.False(t, ok)

	schema = namesTestSchema(true)
	require.NoError(t, schema.LoadFormattedNames("v1.0.0-beta.21", camelUpper, pascalUpper, pascalCap))
	for name, want := range map[NameFormat]map[string]string{
		camelUpper:  {"httpClient": "httpClient", "insecureSkipTLSVerify": "insecureSkipTLSVerify", "LLMContentBlockInput": "llmContentBlockInput"},
		pascalUpper: {"httpClient": "HTTPClient", "commitSHA": "CommitSHA", "SHARED": "Shared"},
		pascalCap:   {"httpClient": "HttpClient", "LLMContentBlockInput": "LlmContentBlockInput"},
	} {
		for in, out := range want {
			got, ok := schema.FormattedName(in, name)
			require.True(t, ok, "%s in %s", in, name)
			require.Equal(t, out, got, "%s in %s", in, name)
		}
	}
	// names the engine can't format are left out
	_, ok = schema.FormattedName("_", camelUpper)
	require.False(t, ok)

	// filtered schemas keep the formatted names
	got, ok := schema.Exclude("someModule").FormattedName("httpClient", pascalUpper)
	require.True(t, ok)
	require.Equal(t, "HTTPClient", got)
}

func TestFormatNamesUnknownFormat(t *testing.T) {
	_, err := FormatNames([]string{"x"}, NameFormat{Casing: "NOPE", Acronyms: AcronymsUppercase}, "")
	require.Error(t, err)
}

// TestFormatIdentifiersParity checks the vendored naming rules against a live
// engine's Query.formatIdentifiers, when the test runs in a nested exec of an
// engine that has it (DAGGER_SESSION_PORT set, a v1.0.0-beta.21+ engine).
func TestFormatIdentifiersParity(t *testing.T) {
	port := os.Getenv("DAGGER_SESSION_PORT")
	if port == "" {
		t.Skip("no engine session (DAGGER_SESSION_PORT unset)")
	}
	names := []string{
		"shortSha", "parentShas", "withGPU", "http", "sshfsVolume", "llm",
		"pushUrl", "callId", "getUrl", "comUrl", "withHttpPrefix", "MyModHttpclient",
		"TCP", "OBJECT_KIND", "GITHUB_REPO", "PerSession", "LLMContentBlockKind",
		"JSONValue", "IPv6Address", "insecureSkipTLSVerify", "E2ETest",
	}
	query := func(q string, vars map[string]any, out any) error {
		body, err := json.Marshal(map[string]any{"query": q, "variables": vars})
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/query", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(os.Getenv("DAGGER_SESSION_TOKEN"), "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return json.NewDecoder(resp.Body).Decode(out)
	}

	var probe struct {
		Data struct {
			Type struct {
				Fields []struct{ Name string } `json:"fields"`
			} `json:"__type"`
		} `json:"data"`
	}
	require.NoError(t, query(`{ __type(name: "Query") { fields { name } } }`, nil, &probe))
	found := false
	for _, f := range probe.Data.Type.Fields {
		found = found || f.Name == "formatIdentifiers"
	}
	if !found {
		t.Skip("engine session has no Query.formatIdentifiers")
	}

	for _, f := range []NameFormat{camelUpper, pascalUpper, pascalCap} {
		var res struct {
			Data struct {
				FormatIdentifiers []string `json:"formatIdentifiers"`
			} `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		require.NoError(t, query(
			`query($names: [String!]!, $casing: Casing!, $acronyms: AcronymStyle) { formatIdentifiers(names: $names, casing: $casing, acronyms: $acronyms) }`,
			map[string]any{"names": names, "casing": string(f.Casing), "acronyms": string(f.Acronyms)},
			&res,
		))
		require.Empty(t, res.Errors)
		local, err := FormatNames(names, f, "")
		require.NoError(t, err)
		for i, name := range names {
			require.Equal(t, res.Data.FormatIdentifiers[i], local[name], "%s in %s", name, f)
		}
	}
}
