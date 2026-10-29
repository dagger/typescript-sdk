package identifiers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Engine runs GraphQL queries against the Dagger engine.
type Engine interface {
	// Query runs query with variables and decodes the response's data into
	// data. GraphQL errors are returned as an error.
	Query(ctx context.Context, query string, variables map[string]any, data any) error
}

// Connector applies the gate: it hands out Names for a version only when the
// version and the engine are both past CasingVersion. It connects to the
// engine, and asks for its release, at most once, and only when a version
// past CasingVersion needs it.
type Connector struct {
	dial func() (Engine, error)

	once    sync.Once
	engine  Engine
	applies bool
	err     error

	mu    sync.Mutex
	names map[string]*Names
}

// NewConnector returns a Connector that connects with dial.
func NewConnector(dial func() (Engine, error)) *Connector {
	return &Connector{dial: dial, names: map[string]*Names{}}
}

// SessionConnector returns a Connector to the session of the environment
// (see SessionFromEnv).
func SessionConnector() *Connector {
	return NewConnector(func() (Engine, error) { return SessionFromEnv() })
}

// Names returns the Names for code generated at version: a schema's
// __schemaVersion, or the engine version a module's declarations are named
// by. It is nil, the legacy rules, when version is before CasingVersion,
// without connecting, or when the engine's release is. Each version has one
// Names, so everything generated at a version shares its formatted names.
func (c *Connector) Names(ctx context.Context, version string) (*Names, error) {
	if !Applies(version) {
		return nil, nil
	}
	applies, err := c.EngineApplies(ctx)
	if err != nil {
		return nil, fmt.Errorf("naming identifiers at %s depends on the engine's release: %w", version, err)
	}
	if !applies {
		// An older engine names every module by strcase, and its schemas
		// report every v1.0.0 prerelease as v1.0.0, past CasingVersion.
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.names[version] == nil {
		c.names[version] = NewNames(c.engine, version)
	}
	return c.names[version], nil
}

// EngineApplies reports whether the engine's release is at or past
// CasingVersion: whether it names modules by its identifier naming rules at
// all. It connects to the engine.
func (c *Connector) EngineApplies(ctx context.Context) (bool, error) {
	c.once.Do(func() {
		c.engine, c.err = c.dial()
		if c.err != nil {
			c.err = fmt.Errorf("connect to the engine to name identifiers: %w", c.err)
			return
		}
		var release string
		release, c.err = EngineVersion(ctx, c.engine)
		c.applies = Applies(release)
	})
	return c.applies, c.err
}

// EngineVersion returns the engine's release: Query.version.
func EngineVersion(ctx context.Context, engine Engine) (string, error) {
	var data struct {
		Version string `json:"version"`
	}
	if err := engine.Query(ctx, "query EngineVersion { version }", nil, &data); err != nil {
		return "", fmt.Errorf("query engine version: %w", err)
	}
	return data.Version, nil
}

// request is one formatIdentifiers call of a batch.
type request struct {
	format  Format
	version string
	names   []string
}

// formatIdentifiers formats every request's names in a single query, each
// request an aliased formatIdentifiers field, and returns the formatted names
// in request and input order.
func formatIdentifiers(ctx context.Context, engine Engine, requests []request) ([][]string, error) {
	var params, fields strings.Builder
	variables := map[string]any{}
	versions := map[string]string{} // version -> variable
	for i, r := range requests {
		v, ok := versions[r.version]
		if !ok {
			v = fmt.Sprintf("v%d", len(versions))
			versions[r.version] = v
			fmt.Fprintf(&params, ", $%s: String!", v)
			variables[v] = r.version
		}
		fmt.Fprintf(&params, ", $n%d: [String!]!", i)
		fmt.Fprintf(&fields, "  f%d: formatIdentifiers(names: $n%d, casing: %s, acronyms: %s, version: $%s)\n",
			i, i, r.format.Casing, r.format.Acronyms, v)
		variables[fmt.Sprintf("n%d", i)] = r.names
	}
	query := fmt.Sprintf("query FormatIdentifiers(%s) {\n%s}", strings.TrimPrefix(params.String(), ", "), fields.String())
	var data map[string][]string
	if err := engine.Query(ctx, query, variables, &data); err != nil {
		return nil, fmt.Errorf("format identifiers: %w", err)
	}
	formatted := make([][]string, len(requests))
	for i, r := range requests {
		names := data[fmt.Sprintf("f%d", i)]
		if len(names) != len(r.names) {
			return nil, fmt.Errorf("format identifiers: got %d names for %d", len(names), len(r.names))
		}
		formatted[i] = names
	}
	return formatted, nil
}

// Session is the Dagger session a command runs in: the engine's GraphQL
// endpoint on localhost, which the engine provides to a command it runs as a
// nested client (withExec with experimentalPrivilegedNesting, the default in
// the v1 API).
type Session struct {
	url    string
	token  string
	client *http.Client
}

// SessionFromEnv returns the session of DAGGER_SESSION_PORT and
// DAGGER_SESSION_TOKEN.
func SessionFromEnv() (*Session, error) {
	port := os.Getenv("DAGGER_SESSION_PORT")
	token := os.Getenv("DAGGER_SESSION_TOKEN")
	if port == "" || token == "" {
		return nil, errors.New("no Dagger session: DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN are not set; run the command as a nested client of a Dagger session")
	}
	return &Session{url: "http://127.0.0.1:" + port + "/query", token: token, client: http.DefaultClient}, nil
}

// Query implements Engine.
func (s *Session) Query(ctx context.Context, query string, variables map[string]any, data any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(s.token, "")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response (HTTP %s): %w", resp.Status, err)
	}
	if len(result.Errors) > 0 {
		messages := make([]string, len(result.Errors))
		for i, e := range result.Errors {
			messages[i] = e.Message
		}
		return errors.New(strings.Join(messages, "; "))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return json.Unmarshal(result.Data, data)
}
