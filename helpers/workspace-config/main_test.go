package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const repro = `
[modules]

[modules.typescript-sdk]
source = "dagger.io/sdk/typescript@feat/unified-clients"

[sdks.typescript]
module = "typescript-sdk"

[modules.test]
source = ".dagger/modules/test"

[sdks.typescript.scopes.".dagger/modules/test"]
is-module = true
name = "test"
clients = ["github.com/shykes/daggerverse/hello"]

[sdks.typescript.scopes.".dagger/modules/test".settings]
entrypoint = true

[sdks.typescript.scopes.my-code]
clients = ["./.dagger/modules/test"]
`

func decode(t *testing.T, out string) [][]string {
	t.Helper()
	var decoded [][]string
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	return decoded
}

func TestScopesSelectsSDKByModule(t *testing.T) {
	out, err := scopesJSON([]byte(repro), "typescript-sdk")
	require.NoError(t, err)

	scopes := decode(t, out)
	require.Equal(t, [][]string{
		{".dagger/modules/test", "true", "test", "true", "github.com/shykes/daggerverse/hello"},
		{"my-code", "false", "", "false", "./.dagger/modules/test"},
	}, scopes)
}

func TestScopesIgnoresOtherSDKs(t *testing.T) {
	out, err := scopesJSON([]byte(repro), "python-sdk")
	require.NoError(t, err)
	require.Empty(t, decode(t, out))
}

func TestScopesSDKWideSettingApplies(t *testing.T) {
	cfg := `
[sdks.ts]
module = "typescript-sdk"

[sdks.ts.settings]
entrypoint = true

[sdks.ts.scopes.app]
is-module = true
name = "app"

[sdks.ts.scopes.legacy]
is-module = true
name = "legacy"

[sdks.ts.scopes.legacy.settings]
entrypoint = false
`
	out, err := scopesJSON([]byte(cfg), "typescript-sdk")
	require.NoError(t, err)

	scopes := decode(t, out)
	require.Equal(t, [][]string{
		{"app", "true", "app", "true"},
		{"legacy", "true", "legacy", "false"},
	}, scopes, "the SDK-wide setting reaches a scope without its own; a scope's own setting shadows it")
}

func TestScopesRootKeyCleans(t *testing.T) {
	cfg := `
[sdks.ts]
module = "typescript-sdk"

[sdks.ts.scopes."."]
is-module = true
name = "root"
`
	out, err := scopesJSON([]byte(cfg), "typescript-sdk")
	require.NoError(t, err)

	scopes := decode(t, out)
	require.Equal(t, [][]string{{".", "true", "root", "false"}}, scopes)
}
