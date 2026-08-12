package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// minimalSchema is the smallest introspection response the generators accept:
// a Query type and nothing else. The generators' own behaviour is covered by
// the golden tests; these cases are about the CLI around them.
const minimalSchema = `{
  "__schema": {
    "queryType": {"name": "Query"},
    "types": [{"kind": "OBJECT", "name": "Query", "fields": []}]
  },
  "__schemaVersion": "v0.21.0"
}`

func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestRunDispatch(t *testing.T) {
	dir := t.TempDir()
	schema := writeFile(t, dir, "schema.json", minimalSchema)

	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no subcommand",
			args:    nil,
			wantErr: "usage: codegen",
		},
		{
			name:    "unknown subcommand",
			args:    []string{"generate-module"},
			wantErr: `unknown command "generate-module"`,
		},
		{
			// Each generator reads the schema from a file; without one there is
			// nothing to generate from, and defaulting to an empty schema would
			// silently emit bindings with no API in them.
			name:    "module without a schema",
			args:    []string{"module", "--module-name", "app"},
			wantErr: "--introspection-json-path is required",
		},
		{
			// The module name decides which types stay in client.gen.ts and
			// which split into per-dependency files, so it cannot be inferred.
			name:    "module without a name",
			args:    []string{"module", "--introspection-json-path", schema},
			wantErr: "--module-name is required",
		},
		{
			name:    "client without a schema",
			args:    []string{"client"},
			wantErr: "--introspection-json-path is required",
		},
		{
			name:    "library without a schema",
			args:    []string{"library"},
			wantErr: "--introspection-json-path is required",
		},
		{
			// The entrypoint is rendered from the scan of the user's source, not
			// from the schema — it is the one generator with a different input.
			name:    "entrypoint without a typedef",
			args:    []string{"entrypoint"},
			wantErr: "--typedef-json-path is required",
		},
		{
			name:    "schema file that does not exist",
			args:    []string{"library", "--introspection-json-path", filepath.Join(dir, "absent.json")},
			wantErr: "read introspection json",
		},
		{
			name:    "schema file that is not introspection output",
			args:    []string{"library", "--introspection-json-path", writeFile(t, dir, "empty.json", `{}`)},
			wantErr: "introspection json has no __schema",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// TestRunModule covers the module subcommand end to end, including the overlay
// writing the result to disk — the step every generator finishes with and which
// no other test exercises.
func TestRunModule(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")

	err := run([]string{
		"module",
		"--introspection-json-path", writeFile(t, dir, "schema.json", minimalSchema),
		"--module-name", "app",
		"--output", out,
	})
	require.NoError(t, err)

	// Flat in the output directory: the caller lays this down as the module's
	// sdk/, so a nested path would land the bindings where nothing looks.
	contents, err := os.ReadFile(filepath.Join(out, "client.gen.ts"))
	require.NoError(t, err)
	require.Contains(t, string(contents), `from "./core.js"`)
}

func TestRunClient(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	meta := writeFile(t, dir, "meta.json", `{
	  "moduleName": "app",
	  "engineVersion": "v1.0.0-beta.9",
	  "module": {"kind": "DIR_SOURCE", "path": ".dagger/modules/app"}
	}`)

	err := run([]string{
		"client",
		"--introspection-json-path", writeFile(t, dir, "schema.json", minimalSchema),
		"--client-meta-path", meta,
		"--output", out,
	})
	require.NoError(t, err)

	contents, err := os.ReadFile(filepath.Join(out, "dagger.gen.ts"))
	require.NoError(t, err)
	require.Contains(t, string(contents), `from "@dagger.io/dagger"`)
}

// TestRunClientRejectsUnservableModule guards the fail-closed check: a client
// serves exactly the module it binds, and a kind with no serve path would
// produce a client that builds and then cannot reach its module at runtime.
func TestRunClientRejectsUnservableModule(t *testing.T) {
	dir := t.TempDir()
	meta := writeFile(t, dir, "meta.json", `{"moduleName": "app", "module": {"kind": "WAT"}}`)

	err := run([]string{
		"client",
		"--introspection-json-path", writeFile(t, dir, "schema.json", minimalSchema),
		"--client-meta-path", meta,
		"--output", filepath.Join(dir, "out"),
	})
	require.ErrorContains(t, err, `unsupported source kind "WAT"`)
}

func TestRunLibrary(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")

	err := run([]string{
		"library",
		"--introspection-json-path", writeFile(t, dir, "schema.json", minimalSchema),
		"--output", out,
	})
	require.NoError(t, err)

	// The library's bindings ship inside the library, so they reach the runtime
	// by relative path rather than through the bundle or the package name.
	contents, err := os.ReadFile(filepath.Join(out, "client.gen.ts"))
	require.NoError(t, err)
	require.Contains(t, string(contents), `from "../common/context.js"`)
}

func TestRunEntrypoint(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")

	typedef, err := json.Marshal(map[string]any{
		"name": "App",
		"objects": map[string]any{
			"App": map[string]any{
				"name":       "App",
				"kind":       "class",
				"isExported": true,
				"location":   map[string]any{"filepath": "src/index.ts", "line": 3, "column": 14},
				"methods": map[string]any{
					"hello": map[string]any{
						"name":       "hello",
						"returnType": map[string]any{"kind": "STRING_KIND"},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, run([]string{
		"entrypoint",
		"--typedef-json-path", writeFile(t, dir, "typedef.json", string(typedef)),
		"--output", out,
		"--module-root", "/work",
	}))

	contents, err := os.ReadFile(filepath.Join(out, "__dagger.entrypoint.ts"))
	require.NoError(t, err)
	require.Contains(t, string(contents), `import { App } from "./src/index"`)
	require.Contains(t, string(contents), `case "hello":`)
}
