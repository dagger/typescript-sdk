package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// sessionServer stands in for the nested session's GraphQL endpoint and points
// the env vars the engine injects at it, so these cases exercise the real
// request path rather than a seam.
func sessionServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	parsed, err := url.Parse(srv.URL)
	require.NoError(t, err)

	t.Setenv(sessionPortEnv, parsed.Port())
	t.Setenv(sessionTokenEnv, "test-token")
}

func TestRunIntrospect(t *testing.T) {
	var gotQuery, gotUser string
	sessionServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		gotUser = user

		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query  string `json:"query"`
			OpName string `json:"operationName"`
		}
		_ = json.Unmarshal(body, &req)
		gotQuery = req.Query

		_, _ = w.Write([]byte(`{"data": {"__schema": {"queryType": {"name": "Query"}}}}`))
	})

	out := filepath.Join(t.TempDir(), "schema.json")
	require.NoError(t, runIntrospect([]string{"--output", out}))

	// The session authenticates with the token as the basic-auth username.
	require.Equal(t, "test-token", gotUser)
	require.Contains(t, gotQuery, "IntrospectionQuery")

	// Only the payload under "data" is written: that is the shape the
	// generators parse, so unwrapping here keeps them free of the transport.
	contents, err := os.ReadFile(out)
	require.NoError(t, err)
	require.JSONEq(t, `{"__schema": {"queryType": {"name": "Query"}}}`, string(contents))
}

// TestRunIntrospectGraphQLError covers a request that succeeds at the HTTP
// level but fails in the API. Ignoring the errors array would write an empty
// schema and turn a clear failure into bindings with no API in them.
func TestRunIntrospectGraphQLError(t *testing.T) {
	sessionServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors": [{"message": "schema unavailable"}]}`))
	})

	err := runIntrospect([]string{"--output", filepath.Join(t.TempDir(), "schema.json")})
	require.ErrorContains(t, err, "schema unavailable")
}

func TestRunIntrospectEmptyData(t *testing.T) {
	sessionServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	err := runIntrospect([]string{"--output", filepath.Join(t.TempDir(), "schema.json")})
	require.ErrorContains(t, err, "returned no data")
}

func TestRunIntrospectHTTPError(t *testing.T) {
	sessionServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	err := runIntrospect([]string{"--output", filepath.Join(t.TempDir(), "schema.json")})
	require.ErrorContains(t, err, "unexpected status")
}

// TestRunIntrospectOutsideSession covers running without the nested-exec env
// the engine provides. The message has to name the cause, since the fix is to
// change how the exec is declared rather than anything about the command.
func TestRunIntrospectOutsideSession(t *testing.T) {
	t.Setenv(sessionPortEnv, "")
	require.NoError(t, os.Unsetenv(sessionPortEnv))

	err := runIntrospect([]string{"--output", filepath.Join(t.TempDir(), "schema.json")})
	require.ErrorContains(t, err, "experimentalPrivilegedNesting")
}
