package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/typescript/templates"
	"codegen/introspection"
)

// identifierSchemaJSON has names the engine's naming rules recase: a field
// (shortSha), a required arg (callId), an optional arg (pushUrl), a field whose
// Opts type is recased (http), an input object field (callId), which goes over
// the wire as is, and an enum member with a term (GITHUB_SSH). Query's
// formatIdentifiers field is the gate, added by identifierSchema.
const identifierSchemaJSON = `
[
  {
    "kind": "OBJECT",
    "name": "Query",
    "fields": [
      {
        "name": "http",
        "args": [
          {"name": "url", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}},
          {"name": "pushUrl", "description": "Where to push.", "type": {"kind": "SCALAR", "name": "String"}}
        ],
        "type": {"kind": "NON_NULL", "ofType": {"kind": "OBJECT", "name": "GitCommit"}}
      }
    ],
    "inputFields": null,
    "interfaces": [],
    "enumValues": null,
    "possibleTypes": null
  },
  {
    "kind": "OBJECT",
    "name": "GitCommit",
    "fields": [
      {
        "name": "shortSha",
        "args": [],
        "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}
      },
      {
        "name": "withToolResult",
        "args": [
          {"name": "callId", "description": "The tool call.", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}},
          {"name": "block", "type": {"kind": "INPUT_OBJECT", "name": "ToolResultInput"}}
        ],
        "type": {"kind": "NON_NULL", "ofType": {"kind": "OBJECT", "name": "GitCommit"}}
      },
      {
        "name": "method",
        "args": [
          {"name": "fallback", "type": {"kind": "NON_NULL", "ofType": {"kind": "ENUM", "name": "HttpMethod"}}}
        ],
        "type": {"kind": "NON_NULL", "ofType": {"kind": "ENUM", "name": "HttpMethod"}}
      }
    ],
    "inputFields": null,
    "interfaces": [],
    "enumValues": null,
    "possibleTypes": null
  },
  {
    "kind": "INPUT_OBJECT",
    "name": "ToolResultInput",
    "fields": null,
    "inputFields": [
      {"name": "callId", "type": {"kind": "SCALAR", "name": "String"}}
    ],
    "interfaces": [],
    "enumValues": null,
    "possibleTypes": null
  },
  {
    "kind": "ENUM",
    "name": "NetworkProtocol",
    "fields": null,
    "inputFields": null,
    "interfaces": [],
    "enumValues": [
      {"name": "TCP"},
      {"name": "GITHUB_SSH"}
    ],
    "possibleTypes": null
  },
  {
    "kind": "ENUM",
    "name": "HttpMethod",
    "fields": null,
    "inputFields": null,
    "interfaces": [],
    "enumValues": [
      {"name": "GET"}
    ],
    "possibleTypes": null
  },
  {
    "kind": "INTERFACE",
    "name": "Fetcher",
    "fields": [
      {
        "name": "getUrl",
        "args": [],
        "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}
      }
    ],
    "inputFields": null,
    "interfaces": [],
    "enumValues": null,
    "possibleTypes": []
  }
]
`

// identifierSchema loads identifierSchemaJSON, with Query.formatIdentifiers
// when gated, the way the generator does: names are formatted only when the
// schema has the field.
func identifierSchema(t *testing.T, gated bool) *introspection.Schema {
	t.Helper()
	schema := objectsInit(t, identifierSchemaJSON)
	schema.QueryType.Name = "Query"
	if gated {
		query := schema.Query()
		str := &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: "String"}
		list := &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{
			Kind: introspection.TypeKindList, OfType: str,
		}}
		query.Fields = append(query.Fields, &introspection.Field{
			Name:         "formatIdentifiers",
			Args:         introspection.InputValues{{Name: "names", TypeRef: list}},
			TypeRef:      list,
			ParentObject: query,
		})
	}
	require.NoError(t, schema.LoadFormattedNames("v1.0.0-beta.21", templates.NameFormats...))
	generator.SetSchema(&schema)
	t.Cleanup(func() { generator.SetSchema(nil) })
	return &schema
}

func TestEngineFormattedNames(t *testing.T) {
	got := renderAPI(t, identifierSchema(t, true), "v1.0.0-beta.21")

	// methods, with a deprecated alias under the old name
	require.Contains(t, got, "shortSHA = async (): Promise<string> => {")
	require.Contains(t, got, `"shortSha",`)
	require.Contains(t, got, `shortSha: GitCommit["shortSHA"] = (...args) =>`)
	require.Contains(t, got, "@deprecated use shortSHA instead.")

	// required args: the TS name is the parameter, the schema name the key
	require.Contains(t, got, "withToolResult = (callID: string,")
	require.Contains(t, got, "callId:callID,")
	require.Contains(t, got, "@param callID The tool call.")

	// optional args: opts keys are mapped back to the schema name, and the
	// old key is still accepted
	require.Contains(t, got, "export type ClientHTTPOpts = {")
	require.Contains(t, got, "pushURL?: string")
	require.Contains(t, got, "@deprecated use pushURL instead.")
	require.Contains(t, got, "pushUrl?: string")
	require.Contains(t, got, "@param opts.pushURL Where to push.")
	require.Contains(t, got, "...opts, pushURL: undefined, pushUrl: opts?.pushURL ?? opts?.pushUrl")
	require.Contains(t, got, "export type ClientHttpOpts = ClientHTTPOpts")
	require.Contains(t, got, "http = (url: string, opts?: ClientHTTPOpts): GitCommit => {")
	require.Contains(t, got, `"http",`)

	// input object fields go over the wire as they are
	require.Contains(t, got, "export type ToolResultInput = {\n  callId?: string")

	// enum members: PASCAL / CAPITALIZED, values untouched, the old member
	// name kept as a deprecated alias
	require.Contains(t, got, `Tcp = "TCP",`)
	require.Contains(t, got, `GitHubSsh = "GITHUB_SSH",`)
	require.Contains(t, got, "GithubSsh = NetworkProtocol.GitHubSsh,")
	require.Contains(t, got, `return "GITHUB_SSH"`)

	// type names are left alone
	require.Contains(t, got, "export class GitCommit extends BaseClient")
	require.Contains(t, got, "export function NetworkProtocolValueToName(")

	// so are enum converters, which their call sites name after the raw type
	// name, even when a dependency named by the legacy rules brings one the
	// engine would recase
	require.Contains(t, got, "export function HttpMethodValueToName(")
	require.Contains(t, got, "export function HttpMethodNameToValue(")
	require.Contains(t, got, "value_to_name: HttpMethodValueToName")
	require.Contains(t, got, "return HttpMethodNameToValue(response)")
	require.NotContains(t, got, "HTTPMethod")

	// interfaces: the old name is an optional member, so an object
	// implementing the interface structurally needn't spell it; the
	// generated client class always has it
	require.Contains(t, got, "  getURL(): Promise<string>\n")
	require.Contains(t, got, `  getUrl?: Fetcher["getURL"]`)
	require.Contains(t, got, `  getUrl: Fetcher["getURL"] = (...args) =>`)
}

// Without Query.formatIdentifiers (a schema view before v1.0.0-beta.21), every
// name goes through the legacy converters.
func TestEngineFormattedNamesFallback(t *testing.T) {
	schema := identifierSchema(t, false)
	require.Nil(t, schema.FormattedNames)
	got := renderAPI(t, schema, "v1.0.0-beta.20")

	require.Contains(t, got, "shortSha = async (): Promise<string> => {")
	require.NotContains(t, got, "shortSHA")
	require.Contains(t, got, "withToolResult = (callId: string,")
	require.NotContains(t, got, "callID")
	require.Contains(t, got, "export type ClientHttpOpts = {")
	require.NotContains(t, got, "ClientHTTPOpts")
	require.NotContains(t, got, "pushURL")
	require.Contains(t, got, "...opts")
	require.NotContains(t, got, "opts?.")
	require.NotContains(t, got, "@deprecated")
	require.Contains(t, got, `GithubSsh = "GITHUB_SSH",`)
	require.NotContains(t, got, "GitHubSsh")
}
