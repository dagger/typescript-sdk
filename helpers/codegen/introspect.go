package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"codegen/introspection"
)

// Env vars the engine injects into a nested exec, pointing at the session's
// GraphQL endpoint (engine/client/client.go).
const (
	sessionPortEnv  = "DAGGER_SESSION_PORT"
	sessionTokenEnv = "DAGGER_SESSION_TOKEN"
)

// runIntrospect dumps the session's introspection schema.
//
// This is the only subcommand that talks to the engine, and it does so over
// plain HTTP against the nested session rather than through the Go SDK: the
// generation subcommands stay dependency-free and never open a session of their
// own. The schema a plain session serves is core-only — no module is installed
// in it — which is exactly what library bindings are generated from.
func runIntrospect(args []string) error {
	fs := flag.NewFlagSet("introspect", flag.ExitOnError)
	output := fs.String("output", "", "path to write the introspection schema JSON to (default stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	port, ok := os.LookupEnv(sessionPortEnv)
	if !ok {
		return fmt.Errorf("%s is not set: introspect must run in a nested exec (experimentalPrivilegedNesting)", sessionPortEnv)
	}

	body, err := json.Marshal(map[string]string{
		"query":         introspection.Query,
		"operationName": "IntrospectionQuery",
	})
	if err != nil {
		return fmt.Errorf("marshal introspection query: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/query", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build introspection request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(os.Getenv(sessionTokenEnv), "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("introspection query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("introspection query: unexpected status %s", resp.Status)
	}

	// The generators consume the payload under "data" — the same shape as the
	// introspection JSON the engine hands to codegen elsewhere.
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode introspection response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("introspection query: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("introspection query returned no data")
	}

	if *output == "" {
		_, err = os.Stdout.Write(envelope.Data)
		return err
	}

	return os.WriteFile(*output, envelope.Data, 0o644)
}
