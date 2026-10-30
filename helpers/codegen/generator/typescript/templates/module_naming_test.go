package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeModuleNames writes the names the SDK hands codegen: for each table,
// the names asked about and the engine's answers, in the same order.
func writeModuleNames(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "names.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

type pairs struct {
	Names     []string `json:"names"`
	Formatted []string `json:"formatted"`
}

// The engine's answers are looked up as they are; a name it wasn't asked
// about fails the render instead of being guessed.
func TestModuleNames(t *testing.T) {
	names, err := LoadModuleNames(writeModuleNames(t, map[string]any{
		"typeNames":       pairs{[]string{"HttpFetcher"}, []string{"NamingAppHTTPFetcher"}},
		"fieldNames":      pairs{[]string{"E2ETest", "getURL"}, []string{"e2eTest", "getUrl"}},
		"enumMemberNames": pairs{[]string{"Arm64", "HTTP2"}, []string{"ARM64", "HTTP2"}},
		"mainObject":      "NamingApp",
	}))
	require.NoError(t, err)
	require.Equal(t, "NamingApp", names.MainObject)
	require.Equal(t, "NamingAppHTTPFetcher", names.typeName("HttpFetcher"))
	require.Equal(t, "e2eTest", names.fieldName("E2ETest"))
	require.Equal(t, "ARM64", names.enumMemberName("Arm64"))
	require.NoError(t, names.Unanswered())

	names.fieldName("path")
	names.typeName("Fetcher")
	names.fieldName("path")
	require.EqualError(t, names.Unanswered(), "module names: the engine wasn't asked about Fetcher, path")
}

func TestLoadModuleNames(t *testing.T) {
	names, err := LoadModuleNames("")
	require.NoError(t, err)
	require.Equal(t, &ModuleNames{}, names, "no names: nothing answered")

	names, err = LoadModuleNames(writeModuleNames(t, map[string]any{"mainObject": nil}))
	require.NoError(t, err)
	require.Empty(t, names.MainObject, "the engine found no main object")

	_, err = LoadModuleNames(writeModuleNames(t, map[string]any{
		"enumMemberNames": pairs{[]string{"Amd64", "Arm64"}, []string{"AMD64"}},
	}))
	require.ErrorContains(t, err, "enumMemberNames: 1 answers for 2 names")
}
