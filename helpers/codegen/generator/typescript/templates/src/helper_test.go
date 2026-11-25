package test

import (
	"context"
	"flag"
	"os"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/generator/namingtest"
	"codegen/generator/typescript/templates"
)

var updateFixtures = flag.Bool("test.update-fixtures", false, "update the test fixtures")

func updateAndGetFixtures(t *testing.T, filepath, got string) string {
	t.Helper()
	if *updateFixtures {
		err := os.WriteFile(filepath, []byte(got), 0o600)
		require.NoError(t, err)
	}
	want, err := os.ReadFile(filepath)
	require.NoError(t, err)

	return string(want)
}

func templateHelper(t *testing.T) *template.Template {
	t.Helper()
	return templates.New("", nil, "", generator.Config{}, identifiers(t))
}

// identifiers styles the identifiers the bindings generate as the engine does
// (namingtest.Styles), each name as it is looked up.
func identifiers(t *testing.T) *templates.Identifiers {
	t.Helper()
	ids, err := templates.LoadIdentifiers(context.Background(), &namingtest.Engine{Pascal: namingtest.Styles}, "", nil)
	require.NoError(t, err)
	return ids
}
