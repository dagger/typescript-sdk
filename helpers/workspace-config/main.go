// workspace-config reads the engine-owned workspace configuration
// (dagger.toml) for the parts generation needs and cannot get from the engine:
// the full recorded scope list of one SDK. The engine hands generateScope only
// its own scope's clients, and a generate run from a subdirectory plans only
// the scopes around the cwd — while the shared client directory is the union
// over every scope, so pruning it correctly needs them all.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// usage: workspace-config scopes <dagger.toml> <sdk-module-name>
func run(args []string) error {
	if len(args) != 3 || args[0] != "scopes" {
		return fmt.Errorf("usage: workspace-config scopes <dagger.toml> <sdk-module-name>")
	}
	contents, err := os.ReadFile(args[1])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[1], err)
	}
	out, err := scopesJSON(contents, args[2])
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

type workspaceConfig struct {
	SDKs map[string]sdkConfig `toml:"sdks"`
}

type sdkConfig struct {
	Module   string                 `toml:"module"`
	Settings map[string]any         `toml:"settings"`
	Scopes   map[string]scopeConfig `toml:"scopes"`
}

type scopeConfig struct {
	IsModule bool           `toml:"is-module"`
	Name     string         `toml:"name"`
	Clients  []string       `toml:"clients"`
	Settings map[string]any `toml:"settings"`
}

// scope is one recorded scope of the selected SDK, paths config-dir-relative
// and cleaned. Entrypoint is the scope's effective setting: its own when set,
// the SDK-wide one otherwise — mirroring how the engine layers settings when
// it constructs the SDK for a scope.
type scope struct {
	Path       string   `json:"path"`
	IsModule   bool     `json:"isModule"`
	Name       string   `json:"name"`
	Entrypoint bool     `json:"entrypoint"`
	Clients    []string `json:"clients"`
}

func scopesJSON(contents []byte, sdkModule string) (string, error) {
	var cfg workspaceConfig
	if err := toml.Unmarshal(contents, &cfg); err != nil {
		return "", fmt.Errorf("parse workspace config: %w", err)
	}

	// The SDK is recorded under an alias of the user's choosing; what
	// identifies it is which installed module the alias points at.
	var scopes []scope
	for _, sdk := range cfg.SDKs {
		if sdk.Module != sdkModule {
			continue
		}
		for key, sc := range sdk.Scopes {
			scopes = append(scopes, scope{
				Path:       cleanScopePath(key),
				IsModule:   sc.IsModule,
				Name:       sc.Name,
				Entrypoint: boolSetting(sc.Settings, sdk.Settings, "entrypoint"),
				Clients:    append([]string{}, sc.Clients...),
			})
		}
	}

	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Path < scopes[j].Path })

	// Rows of strings — [path, isModule, name, entrypoint, clients...] — not
	// records: Dang's JSON.decode silently materializes a decoded record's
	// fields as their defaults when the record is a module-declared type, while
	// nested string lists round-trip exactly.
	rows := make([][]string, 0, len(scopes))
	for _, s := range scopes {
		row := []string{s.Path, strconv.FormatBool(s.IsModule), s.Name, strconv.FormatBool(s.Entrypoint)}
		rows = append(rows, append(row, s.Clients...))
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("encode scopes: %w", err)
	}
	return string(encoded), nil
}

func cleanScopePath(key string) string {
	cleaned := path.Clean(strings.TrimPrefix(key, "./"))
	if cleaned == "" || cleaned == "/" {
		return "."
	}
	return cleaned
}

// boolSetting reads a boolean setting, the scope's own value shadowing the
// SDK-wide one. TOML gives a real bool; a string spelling is tolerated because
// settings values pass through the engine as opaque scalars.
func boolSetting(scoped, global map[string]any, key string) bool {
	for _, settings := range []map[string]any{scoped, global} {
		if v, ok := settings[key]; ok {
			switch value := v.(type) {
			case bool:
				return value
			case string:
				return value == "true"
			}
			return false
		}
	}
	return false
}
