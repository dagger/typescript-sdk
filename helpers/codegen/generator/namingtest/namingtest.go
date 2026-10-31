// Package namingtest is a fake engine for testing the generators, which ask
// the engine how it names a module's own declarations (ModuleSource.naming),
// and how it styles the identifiers the bindings generate
// (Query.formatIdentifiers). It answers from tables, and fails on a name they
// don't have, as the engine fails on a name it can't parse.
package namingtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"
)

// Engine answers ModuleSource.naming queries from its tables, for the module
// source with ID SourceID, and formatIdentifiers queries from Pascal.
type Engine struct {
	SourceID string
	// TypeNames, FieldNames and EnumMemberNames map a declared name to the
	// engine's name for it.
	TypeNames, FieldNames, EnumMemberNames map[string]string
	// MainObject is the candidate the engine takes for the main object, or ""
	// for none.
	MainObject string
	// Pascal maps a name to its PascalCase styling, acronyms in capitals.
	Pascal map[string]string

	mu       sync.Mutex
	queries  int
	versions []any
}

// Queries is how many queries the engine answered.
func (e *Engine) Queries() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.queries
}

// Versions are the version arguments formatIdentifiers was asked with, in
// order; nil for none.
func (e *Engine) Versions() []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.versions
}

// selection is a field of ModuleNaming selected with its list variable.
var selection = regexp.MustCompile(`(\w+)\((?:names|candidates): \$(\w+)\)`)

// formatIdentifiers is the selection of Query.formatIdentifiers in the
// casing and acronym style the fake styles in.
var formatIdentifiers = regexp.MustCompile(`formatIdentifiers\(names: \$names, casing: PASCAL, acronyms: UPPERCASE, version: \$version\)`)

// Query implements generator.Engine.
func (e *Engine) Query(_ context.Context, query string, variables map[string]any, data any) error {
	e.mu.Lock()
	e.queries++
	e.mu.Unlock()
	if formatIdentifiers.MatchString(query) {
		return e.format(variables, data)
	}
	if variables["id"] != e.SourceID {
		return json.Unmarshal([]byte(`{"node": null}`), data)
	}
	naming := map[string]any{}
	for _, m := range selection.FindAllStringSubmatch(query, -1) {
		field, names := m[1], strings(variables[m[2]])
		switch field {
		case "mainObject":
			naming[field] = nil
			for _, name := range names {
				if name == e.MainObject {
					naming[field] = name
				}
			}
		default:
			table := map[string]map[string]string{"typeNames": e.TypeNames, "fieldNames": e.FieldNames, "enumMemberNames": e.EnumMemberNames}[field]
			answers := make([]string, len(names))
			for i, name := range names {
				answer, ok := table[name]
				if !ok {
					return fmt.Errorf("fake engine: can't name %q (%s)", name, field)
				}
				answers[i] = answer
			}
			naming[field] = answers
		}
	}
	resp, err := json.Marshal(map[string]any{"node": map[string]any{"naming": naming}})
	if err != nil {
		return err
	}
	return json.Unmarshal(resp, data)
}

// format answers a formatIdentifiers query from Pascal.
func (e *Engine) format(variables map[string]any, data any) error {
	e.mu.Lock()
	e.versions = append(e.versions, variables["version"])
	e.mu.Unlock()
	names := strings(variables["names"])
	answers := make([]string, len(names))
	for i, name := range names {
		answer, ok := e.Pascal[name]
		if !ok {
			return fmt.Errorf("fake engine: can't style %q", name)
		}
		answers[i] = answer
	}
	resp, err := json.Marshal(map[string]any{"formatIdentifiers": answers})
	if err != nil {
		return err
	}
	return json.Unmarshal(resp, data)
}

// Styles are the engine's PascalCase styling, acronyms in capitals, of the
// names the generators' test schemas generate identifiers from: what a
// v1.0.0-beta.22 engine's formatIdentifiers answered for them, at every
// version the tests ask at.
var Styles = map[string]string{
	// Module file names, which the client import aliases are made from.
	"app": "App", "client": "Client", "dep": "Dep", "docs": "Docs", "gendep": "Gendep",
	"hello": "Hello", "my-dep": "MyDep", "payments": "Payments", "plain": "Plain",
	// Enum members.
	"TCP": "TCP", "UDP": "UDP", "VALUE": "Value",
	"Gzip": "Gzip", "Zstd": "Zstd", "EStarGZ": "EStarGZ", "Uncompressed": "Uncompressed",
	"GZIP": "Gzip", "ZSTD": "Zstd", "ESTARGZ": "Estargz", "UNCOMPRESSED": "Uncompressed",
}

// strings is a list variable, as the generators pass it or as it comes off
// the wire.
func strings(v any) []string {
	switch v := v.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, len(v))
		for i, s := range v {
			out[i], _ = s.(string)
		}
		return out
	}
	return nil
}

// ServeHTTP serves the engine as a session's GraphQL endpoint.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var data any
	resp := map[string]any{}
	if err := e.Query(r.Context(), req.Query, req.Variables, &data); err != nil {
		resp["errors"] = []map[string]string{{"message": err.Error()}}
	} else {
		resp["data"] = data
	}
	_ = json.NewEncoder(w).Encode(resp)
}
