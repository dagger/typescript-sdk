package main

import (
	"io"
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

// TestRunGetIncludePrintsLines pins the wire format the SDK reads back: one
// resolved pattern per line, nothing at all when the field is absent, and a
// non-nil error rather than a partial list when an entry is invalid.
func TestRunGetIncludePrintsLines(t *testing.T) {
	p := writeTemp(t, `{"dagger":{"include":["../lib"]}}`)
	out := captureStdout(t, func() {
		require.NoError(t, run([]string{"get-include", p, "apps/web"}))
	})
	require.Equal(t, "apps/lib\napps/lib/**\n", out)

	p = writeTemp(t, samplePackageJSON)
	out = captureStdout(t, func() {
		require.NoError(t, run([]string{"get-include", p, "apps/web"}))
	})
	require.Empty(t, out)

	p = writeTemp(t, `{"dagger":{"include":["/abs"]}}`)
	require.Error(t, run([]string{"get-include", p, "apps/web"}))

	// The module path is the second argument, and resolution needs it.
	p = writeTemp(t, `{"dagger":{"include":["../lib"]}}`)
	require.Error(t, run([]string{"get-include", p}))
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	fn()
	require.NoError(t, w.Close())

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
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
