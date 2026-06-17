package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "package.json")
	require.NoError(t, os.WriteFile(p, []byte(contents), 0o644))
	return p
}

func TestRunSetThenGet(t *testing.T) {
	p := writeTemp(t, samplePackageJSON)

	require.NoError(t, run([]string{"set-base-image", p, "node:23.2.0-alpine"}))

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Contains(t, string(data), "node:23.2.0-alpine")
}

func TestRunGetDoesNotWrite(t *testing.T) {
	p := writeTemp(t, configuredPackageJSON)
	before, err := os.ReadFile(p)
	require.NoError(t, err)

	require.NoError(t, run([]string{"get-package-manager", p}))

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, before, after, "get-* must not modify the file")
}

func TestRunUnknownCommand(t *testing.T) {
	p := writeTemp(t, samplePackageJSON)
	require.Error(t, run([]string{"bogus", p}))
}

func TestRunRequiresValue(t *testing.T) {
	p := writeTemp(t, samplePackageJSON)
	require.Error(t, run([]string{"set-base-image", p}))
}

func TestRunOnMissingFileTreatsAsEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "package.json")

	require.NoError(t, run([]string{"set-base-image", p, "node:23.2.0-alpine"}))

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, "node:23.2.0-alpine", getBaseImage(string(data)))
}
