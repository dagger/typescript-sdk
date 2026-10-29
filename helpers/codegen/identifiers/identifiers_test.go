package identifiers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/identifiers"
	"codegen/identifiers/identifierstest"
)

func TestApplies(t *testing.T) {
	for version, want := range map[string]bool{
		"":                                false,
		"not-a-version":                   false,
		"v0.21.9":                         false,
		"v1.0.0-0":                        false,
		"v1.0.0-beta.20":                  false,
		"v1.0.0-beta.21":                  false,
		"v1.0.0-beta.21+abc123":           false,
		"v1.0.0-beta.22":                  true,
		"1.0.0-beta.22":                   true,
		"v1.0.0-beta.22+441d95c2":         true,
		"v1.0.0-beta.23":                  true,
		"v1.0.0-rc.1":                     true,
		"v1.0.0":                          true,
		"v1.0.0-dev-0123456789ab":         true, // a dev build counts as its release
		"v1.0.0-beta.22-dev-0123456789ab": true,
		"v1.1.0":                          true,
	} {
		require.Equal(t, want, identifiers.Applies(version), version)
	}
}

func TestValid(t *testing.T) {
	require.True(t, identifiers.Valid("commitSha"))
	require.True(t, identifiers.Valid("__typename"))
	require.True(t, identifiers.Valid("3"))
	require.False(t, identifiers.Valid(""))
	require.False(t, identifiers.Valid("__"))
	require.False(t, identifiers.Valid("naïve"))
	require.False(t, identifiers.Valid(strings.Repeat("a", 257)))
	require.True(t, identifiers.Valid(strings.Repeat("a", 256)))
}

var table = identifierstest.Table{
	"PASCAL/UPPERCASE": {
		"commitSha": "CommitSHA",
		"shortSha":  "ShortSHA",
		"JSONValue": "JSONValue",
	},
	"CAMEL/CAPITALIZED": {
		"HTTPClient": "httpClient",
		"GetURL":     "getUrl",
	},
}

// Before the cut, nothing connects: the legacy rules need no engine.
func TestConnectorLegacySchemaDoesNotConnect(t *testing.T) {
	c := identifiers.NewConnector(func() (identifiers.Engine, error) {
		return nil, errors.New("must not connect")
	})
	for _, version := range []string{"", "v0.21.9", "v1.0.0-beta.21"} {
		names, err := c.Names(t.Context(), version)
		require.NoError(t, err)
		require.Nil(t, names)
	}
}

// Engines before the cut name modules by strcase, and report every v1.0.0
// prerelease view as v1.0.0: the schema's version alone can't tell.
func TestConnectorOlderEngineKeepsLegacyRules(t *testing.T) {
	for _, release := range []string{"v1.0.0-beta.19", "v1.0.0-beta.21+abc", "v0.21.9"} {
		engine := &identifierstest.Engine{Release: release, Table: table}
		names, err := engine.Connector().Names(t.Context(), "v1.0.0")
		require.NoError(t, err)
		require.Nil(t, names, release)
		require.Equal(t, 0, engine.FormatQueries())
	}
}

func TestConnectorAppliesPastTheCut(t *testing.T) {
	for _, release := range []string{"v1.0.0-beta.22", "v1.0.0-beta.22+441d95c2", "v1.0.0-dev-0123456789ab"} {
		engine := &identifierstest.Engine{Release: release, Table: table}
		c := engine.Connector()
		names, err := c.Names(t.Context(), "v1.0.0-beta.22")
		require.NoError(t, err)
		require.NotNil(t, names, release)
		require.Equal(t, "v1.0.0-beta.22", names.Version())
		again, err := c.Names(t.Context(), "v1.0.0-beta.22")
		require.NoError(t, err)
		require.Same(t, names, again, "a version has one Names")
		require.Len(t, engine.Queries(), 1, "the engine's release is asked once")
	}
}

// A schema past the cut can't be generated without the engine: silently
// keeping the legacy rules would produce bindings the engine doesn't match.
func TestConnectorPastTheCutNeedsAnEngine(t *testing.T) {
	t.Setenv("DAGGER_SESSION_PORT", "")
	t.Setenv("DAGGER_SESSION_TOKEN", "")
	_, err := identifiers.SessionConnector().Names(t.Context(), "v1.0.0-beta.22")
	require.ErrorContains(t, err, "DAGGER_SESSION_PORT")
}

func TestNilNamesAreTheLegacyRules(t *testing.T) {
	var names *identifiers.Names
	_, ok := names.Lookup(identifiers.TypeName, "commitSha")
	require.False(t, ok)
	runs := 0
	require.NoError(t, names.Run(t.Context(), func() error { runs++; return nil }))
	require.Equal(t, 1, runs)
	require.Empty(t, names.Version())
}

// Names looked up while rendering are formatted in one query per round, each
// name once, and the render runs again with them.
func TestRunBatchesLookups(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	var rendered []string
	render := func() error {
		rendered = rendered[:0]
		for _, lookup := range []struct {
			f    identifiers.Format
			name string
		}{
			{identifiers.TypeName, "commitSha"},
			{identifiers.TypeName, "shortSha"},
			{identifiers.TypeName, "commitSha"},
			{identifiers.TypeName, "JSONValue"},
			{identifiers.FieldName, "HTTPClient"},
			{identifiers.FieldName, "GetURL"},
			{identifiers.TypeName, "naïve"}, // never sent
		} {
			got, ok := names.Lookup(lookup.f, lookup.name)
			if !ok {
				got = "fallback:" + lookup.name
			}
			rendered = append(rendered, got)
		}
		return nil
	}
	require.NoError(t, names.Run(t.Context(), render))
	require.Equal(t, []string{"CommitSHA", "ShortSHA", "CommitSHA", "JSONValue", "httpClient", "getUrl", "fallback:naïve"}, rendered)
	require.Equal(t, 1, engine.FormatQueries())
	query := engine.Queries()[0]
	require.Contains(t, query, "query FormatIdentifiers($v0: String!, $n0: [String!]!, $n1: [String!]!)")
	require.Contains(t, query, "f0: formatIdentifiers(names: $n0, casing: CAMEL, acronyms: CAPITALIZED, version: $v0)")
	require.Contains(t, query, "f1: formatIdentifiers(names: $n1, casing: PASCAL, acronyms: UPPERCASE, version: $v0)")

	// Everything is formatted now: another run needs no query.
	require.NoError(t, names.Run(t.Context(), render))
	require.Equal(t, 1, engine.FormatQueries())
}

// A render looking names up in Names of several versions formats them all in
// one query, each request at its own version; nil Names are skipped.
func TestRunSeveralNames(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.23", Table: table}
	c := engine.Connector()
	schemaNames, err := c.Names(t.Context(), "v1.0.0-beta.23")
	require.NoError(t, err)
	moduleNames, err := c.Names(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	var got []string
	require.NoError(t, identifiers.Run(t.Context(), func() error {
		goName, _ := schemaNames.Lookup(identifiers.TypeName, "commitSha")
		field, _ := moduleNames.Lookup(identifiers.FieldName, "HTTPClient")
		got = []string{goName, field}
		return nil
	}, schemaNames, nil, moduleNames, schemaNames))
	require.Equal(t, []string{"CommitSHA", "httpClient"}, got)
	require.Equal(t, 1, engine.FormatQueries())
	query := engine.Queries()[1]
	require.Contains(t, query, "query FormatIdentifiers($v0: String!, $n0: [String!]!, $v1: String!, $n1: [String!]!)")
}

// Prefetched names are formatted before the first render, so a render that
// only needs them runs once.
func TestRunPrefetch(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	names.Prefetch(identifiers.TypeName, "commitSha", "shortSha")
	runs := 0
	require.NoError(t, names.Run(t.Context(), func() error {
		runs++
		_, ok := names.Lookup(identifiers.TypeName, "shortSha")
		require.True(t, ok)
		return nil
	}))
	require.Equal(t, 1, runs)
	require.Equal(t, 1, engine.FormatQueries())
}

// A render that derives names from formatted ones takes a round per step.
func TestRunChainedLookups(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: identifierstest.Table{
		"CAMEL/CAPITALIZED": {"GetURL": "getUrl"},
		// The stand-in is formatted too; it's wasted, but harmless.
		"PASCAL/UPPERCASE": {"getUrl": "GetURL", "getURL": "GetURL"},
	}}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	var got string
	require.NoError(t, names.Run(t.Context(), func() error {
		field, ok := names.Lookup(identifiers.FieldName, "GetURL")
		if !ok {
			field = "getURL" // the legacy spelling stands in until formatted
		}
		got, _ = names.Lookup(identifiers.TypeName, field)
		return nil
	}))
	require.Equal(t, "GetURL", got)
	require.Equal(t, 2, engine.FormatQueries())
}

// A render failing on a stand-in name is retried with the names formatted.
func TestRunRetriesRenderErrorsWithMissingNames(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: table}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	require.NoError(t, names.Run(t.Context(), func() error {
		if _, ok := names.Lookup(identifiers.TypeName, "commitSha"); !ok {
			return errors.New("duplicate declaration")
		}
		return nil
	}))
	err := names.Run(t.Context(), func() error { return errors.New("real failure") })
	require.EqualError(t, err, "real failure")
}

// Run is bounded: a render that keeps asking for new names is a bug.
func TestRunIsBounded(t *testing.T) {
	tbl := identifierstest.Table{"PASCAL/UPPERCASE": {}}
	for i := range 10 {
		tbl["PASCAL/UPPERCASE"][strings.Repeat("a", i+1)] = "A"
	}
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: tbl}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	runs := 0
	err := names.Run(t.Context(), func() error {
		runs++
		names.Lookup(identifiers.TypeName, strings.Repeat("a", runs))
		return nil
	})
	require.ErrorContains(t, err, "still unformatted")
	require.Equal(t, 4, runs)
}

func TestRunPropagatesEngineErrors(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22", Table: identifierstest.Table{}}
	names := identifiers.NewNames(engine, "v1.0.0-beta.22")
	names.Prefetch(identifiers.TypeName, "unknown")
	err := names.Run(t.Context(), func() error { return nil })
	require.ErrorContains(t, err, `no PASCAL/UPPERCASE formatting of "unknown"`)
}

// Session speaks the engine's GraphQL HTTP protocol: POST /query, the
// session token as the basic auth user.
func TestSession(t *testing.T) {
	engine := &identifierstest.Engine{Release: "v1.0.0-beta.22+441d95c2", Table: table}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodPost || r.URL.Path != "/query" || !ok || user != "secret" || pass != "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// JSON decodes lists as []any; the fake wants []string.
		for k, v := range req.Variables {
			if list, ok := v.([]any); ok {
				strs := make([]string, len(list))
				for i, s := range list {
					strs[i] = s.(string)
				}
				req.Variables[k] = strs
			}
		}
		var data map[string]any
		if err := engine.Query(r.Context(), req.Query, req.Variables, &data); err != nil {
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": err.Error()}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	t.Setenv("DAGGER_SESSION_PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	t.Setenv("DAGGER_SESSION_TOKEN", "secret")

	names, err := identifiers.SessionConnector().Names(context.Background(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.NotNil(t, names)
	var got string
	require.NoError(t, names.Run(t.Context(), func() error {
		got, _ = names.Lookup(identifiers.TypeName, "commitSha")
		return nil
	}))
	require.Equal(t, "CommitSHA", got)

	names.Prefetch(identifiers.TypeName, "unknownName")
	err = names.Run(t.Context(), func() error { return nil })
	require.ErrorContains(t, err, `no PASCAL/UPPERCASE formatting of "unknownName"`)
}
