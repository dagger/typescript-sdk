package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const samplePackageJSON = `{
  "type": "module",
  "name": "demo",
  "dependencies": {
    "typescript": "5.9.3"
  }
}`

const configuredPackageJSON = `{
  "type": "module",
  "packageManager": "pnpm@8.15.4",
  "dagger": {
    "baseImage": "node:23.2.0-alpine"
  }
}`

const sampleDenoJSON = `{
  "imports": {
    "@user/lib": "./lib.ts"
  }
}`

func TestGetPackageManager(t *testing.T) {
	require.Equal(t, "", getPackageManager(samplePackageJSON))
	require.Equal(t, "pnpm@8.15.4", getPackageManager(configuredPackageJSON))
	require.Equal(t, "", getPackageManager(""))
}

func TestGetBaseImage(t *testing.T) {
	require.Equal(t, "", getBaseImage(samplePackageJSON))
	require.Equal(t, "node:23.2.0-alpine", getBaseImage(configuredPackageJSON))
	require.Equal(t, "", getBaseImage(sampleDenoJSON))
}

func TestResolveInclude(t *testing.T) {
	const withInclude = `{
  "dagger": {
    "include": ["../lib/greet", "!../lib/greet/testdata", "../shared/*.ts", "../version.txt"]
  }
}`

	patterns, err := resolveInclude(withInclude, "apps/web")
	require.NoError(t, err)
	require.Equal(t, []string{
		// A plain path names itself and everything under it, since the filter
		// cannot tell a file from a directory.
		"apps/lib/greet", "apps/lib/greet/**",
		"!apps/lib/greet/testdata", "!apps/lib/greet/testdata/**",
		// A glob is left alone.
		"apps/shared/*.ts",
		"apps/version.txt", "apps/version.txt/**",
	}, patterns)

	// Absent, empty and non-TypeScript configs all mean "no includes".
	for _, input := range []string{samplePackageJSON, sampleDenoJSON, `{"dagger":{"include":[]}}`, "{}"} {
		patterns, err := resolveInclude(input, "apps/web")
		require.NoError(t, err)
		require.Empty(t, patterns)
	}
}

func TestResolveIncludeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name       string
		entry      string
		modulePath string
		wantErr    string
	}{
		{name: "absolute", entry: `"/etc/passwd"`, modulePath: "app", wantErr: "must be relative to the module directory"},
		{name: "escapes workspace", entry: `"../../outside"`, modulePath: "app", wantErr: "leaves the workspace root"},
		{name: "escapes via climb", entry: `"../app/../../x"`, modulePath: "app", wantErr: "leaves the workspace root"},
		{name: "inside module", entry: `"src/extra"`, modulePath: "app", wantErr: "is inside the module directory"},
		{name: "module itself", entry: `"."`, modulePath: "app", wantErr: "is inside the module directory"},
		{name: "exclude inside module", entry: `"!src/extra"`, modulePath: "app", wantErr: "is inside the module directory"},
		{name: "empty", entry: `""`, modulePath: "app", wantErr: "names no path"},
		{name: "bang only", entry: `"!"`, modulePath: "app", wantErr: "names no path"},
		{name: "not a string", entry: `42`, modulePath: "app", wantErr: "is not a path"},
		{name: "root module", entry: `"../x"`, modulePath: ".", wantErr: "module at the workspace root"},
		{name: "src ancestor", entry: `"../lib"`, modulePath: "src/dagger", wantErr: `under a "src" directory`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveInclude(`{"dagger":{"include":[`+tc.entry+`]}}`, tc.modulePath)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}

	_, err := resolveInclude(`{"dagger":{"include":"../lib"}}`, "app")
	require.ErrorContains(t, err, "must be an array of paths")
}

func TestSetPackageManagerPreservesData(t *testing.T) {
	out, err := setPackageManager(samplePackageJSON, "yarn@1.22.22")
	require.NoError(t, err)
	require.Contains(t, out, `"packageManager":"yarn@1.22.22"`)
	require.Contains(t, out, `"typescript": "5.9.3"`, "unrelated keys were dropped")
}

func TestUnsetPackageManager(t *testing.T) {
	out, err := unsetPackageManager(configuredPackageJSON)
	require.NoError(t, err)
	require.NotContains(t, out, "packageManager")
	require.Contains(t, out, `"baseImage"`, "unrelated dagger config should be preserved")
}

func TestSetBaseImageOnEmpty(t *testing.T) {
	out, err := setBaseImage("{}", "node:23.2.0-alpine")
	require.NoError(t, err)
	require.Equal(t, "node:23.2.0-alpine", getBaseImage(out))
}

func TestSetBaseImagePreservesSiblings(t *testing.T) {
	out, err := setBaseImage(samplePackageJSON, "node:23.2.0-alpine")
	require.NoError(t, err)
	require.Equal(t, "node:23.2.0-alpine", getBaseImage(out))
	require.Contains(t, out, `"typescript": "5.9.3"`, "unrelated keys were dropped")
}

func TestUnsetBaseImagePrunesEmptyDagger(t *testing.T) {
	in := `{"dagger":{"baseImage":"foo"}}`
	out, err := unsetBaseImage(in)
	require.NoError(t, err)
	require.NotContains(t, out, "dagger", "empty dagger object should be pruned")
}

func TestUnsetBaseImageKeepsSiblings(t *testing.T) {
	in := `{"dagger":{"baseImage":"foo","runtime":"node@20.15.0"}}`
	out, err := unsetBaseImage(in)
	require.NoError(t, err)
	require.NotContains(t, out, "baseImage")
	require.Contains(t, out, "runtime", "sibling dagger keys should survive an unset")
}

func TestUnsetBaseImageOnEmptyDoc(t *testing.T) {
	out, err := unsetBaseImage("{}")
	require.NoError(t, err)
	require.Equal(t, "", strings.TrimSpace(strings.Trim(out, "{}")))
}

func TestSetBaseImageOnDenoJSON(t *testing.T) {
	out, err := setBaseImage(sampleDenoJSON, "denoland/deno:alpine")
	require.NoError(t, err)
	require.Equal(t, "denoland/deno:alpine", getBaseImage(out))
	require.Contains(t, out, "@user/lib", "unrelated deno keys should be preserved")
}
