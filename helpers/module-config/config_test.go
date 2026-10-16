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
