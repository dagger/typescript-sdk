// Package session is the Dagger session a codegen command runs in: the
// engine's GraphQL endpoint, which the engine provides to a command it runs as
// a nested client. The entrypoint generators use it to ask how the engine
// names the module's own declarations (ModuleSource.naming).
package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Session is a connection to the engine's GraphQL endpoint on localhost.
type Session struct {
	url    string
	token  string
	client *http.Client
}

// FromEnv returns the session of DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN.
func FromEnv() (*Session, error) {
	port := os.Getenv("DAGGER_SESSION_PORT")
	token := os.Getenv("DAGGER_SESSION_TOKEN")
	if port == "" || token == "" {
		return nil, errors.New("no Dagger session: DAGGER_SESSION_PORT and DAGGER_SESSION_TOKEN are not set; run the command as a nested client of a Dagger session")
	}
	return &Session{url: "http://127.0.0.1:" + port + "/query", token: token, client: http.DefaultClient}, nil
}

// Query runs query with variables and decodes the response's data into data.
// GraphQL errors are returned as an error.
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
