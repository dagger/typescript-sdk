// Package identifierstest provides a fake engine for testing generators that
// name identifiers through the identifiers package. Copied with it from
// github.com/dagger/go-sdk (dagger/go-sdk#90); see package identifiers.
package identifierstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"codegen/identifiers"
)

// Table holds formatted names by format key ("PASCAL/UPPERCASE") and name.
type Table map[string]map[string]string

// Key is the key of a format in a Table.
func Key(f identifiers.Format) string {
	return string(f.Casing) + "/" + string(f.Acronyms)
}

// Load reads a Table from a JSON file.
func Load(path string) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var table Table
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return table, nil
}

// Engine is a fake engine. It answers Query.version with Release and
// Query.formatIdentifiers from its tables: a request's version's table in
// Versions, then Table, then Fallback; without a Fallback, it fails on a name
// no table has, so a test learns of every name its generator formats. It
// records the queries it answers.
type Engine struct {
	Release string
	Table   Table
	// Versions holds the tables of particular versions, for tests that tell
	// the names of one version from another's.
	Versions map[string]Table
	// Fallback formats a name no table has.
	Fallback func(f identifiers.Format, name string) string

	mu      sync.Mutex
	queries []string
}

// Queries returns the queries the engine answered, in order.
func (e *Engine) Queries() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.queries...)
}

// FormatQueries returns how many formatIdentifiers queries the engine
// answered: the generator's round trips for names.
func (e *Engine) FormatQueries() int {
	n := 0
	for _, q := range e.Queries() {
		if strings.Contains(q, "formatIdentifiers") {
			n++
		}
	}
	return n
}

var fieldPattern = regexp.MustCompile(`(f\d+): formatIdentifiers\(names: \$(n\d+), casing: (\w+), acronyms: (\w+), version: \$(v\d+)\)`)

// Query implements identifiers.Engine for the queries the identifiers
// package sends.
func (e *Engine) Query(_ context.Context, query string, variables map[string]any, data any) error {
	e.mu.Lock()
	e.queries = append(e.queries, query)
	e.mu.Unlock()
	result := map[string]any{}
	switch {
	case strings.Contains(query, "formatIdentifiers"):
		for _, m := range fieldPattern.FindAllStringSubmatch(query, -1) {
			version, _ := variables[m[5]].(string)
			if version == "" {
				return fmt.Errorf("fake engine: %s has no version", m[1])
			}
			format := identifiers.Format{Casing: identifiers.Casing(m[3]), Acronyms: identifiers.AcronymStyle(m[4])}
			key := Key(format)
			names, ok := variables[m[2]].([]string)
			if !ok {
				return fmt.Errorf("variable %s is %T, not []string", m[2], variables[m[2]])
			}
			formatted := make([]string, len(names))
			for i, name := range names {
				f, ok := e.Versions[version][key][name]
				if !ok {
					f, ok = e.Table[key][name]
				}
				if !ok && e.Fallback != nil {
					f, ok = e.Fallback(format, name), true
				}
				if !ok {
					return fmt.Errorf("fake engine: no %s formatting of %q", key, name)
				}
				formatted[i] = f
			}
			result[m[1]] = formatted
		}
	case strings.Contains(query, "version"):
		result["version"] = e.Release
	default:
		return fmt.Errorf("fake engine: unexpected query %q", query)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if reflect.ValueOf(data).Kind() != reflect.Pointer {
		return fmt.Errorf("data must be a pointer")
	}
	return json.Unmarshal(encoded, data)
}

// Connector returns a Connector to e.
func (e *Engine) Connector() *identifiers.Connector {
	return identifiers.NewConnector(func() (identifiers.Engine, error) { return e, nil })
}
