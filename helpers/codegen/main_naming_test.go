package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/identifiers"
	"codegen/identifiers/identifierstest"
)

// withConnector stands c in for the session the codegen exec runs nested in.
func withConnector(t *testing.T, c func() *identifiers.Connector) {
	t.Helper()
	prev := connector
	connector = c
	t.Cleanup(func() { connector = prev })
}

// A module named by a version before the cut, or by none, gets the legacy
// names without the engine being asked.
func TestModuleNamesLegacyDoesNotConnect(t *testing.T) {
	withConnector(t, func() *identifiers.Connector {
		return identifiers.NewConnector(func() (identifiers.Engine, error) {
			return nil, errors.New("must not connect")
		})
	})
	for _, version := range []string{"", "v1.0.0-beta.20", "v1.0.0-beta.21", "v0.21.9"} {
		names, err := moduleNames(t.Context(), version)
		require.NoError(t, err, version)
		require.Nil(t, names, version)
	}
}

// Past the cut, the names come from the engine at the module's version, unless
// the engine itself is older: it names every module by strcase.
func TestModuleNamesPastTheCut(t *testing.T) {
	newer := &identifierstest.Engine{Release: "v1.0.0-beta.22+168f78aa"}
	withConnector(t, newer.Connector)
	names, err := moduleNames(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.NotNil(t, names)
	require.Equal(t, "v1.0.0-beta.22", names.Version())

	older := &identifierstest.Engine{Release: "v1.0.0-beta.21"}
	withConnector(t, older.Connector)
	names, err = moduleNames(t.Context(), "v1.0.0-beta.22")
	require.NoError(t, err)
	require.Nil(t, names)
}

// A module version the caller couldn't read fails only on an engine that
// names modules by their version.
func TestModuleNamesUnreadableVersion(t *testing.T) {
	older := &identifierstest.Engine{Release: "v1.0.0-beta.20"}
	withConnector(t, older.Connector)
	names, err := moduleNames(t.Context(), "error: no dagger-module.toml")
	require.NoError(t, err)
	require.Nil(t, names)

	newer := &identifierstest.Engine{Release: "v1.0.0-beta.22"}
	withConnector(t, newer.Connector)
	_, err = moduleNames(t.Context(), "error: no dagger-module.toml")
	require.ErrorContains(t, err, "couldn't be read: no dagger-module.toml")
}

// Past the cut, generating without a session is an error: the legacy names
// could be wrong.
func TestModuleNamesPastTheCutNeedsASession(t *testing.T) {
	t.Setenv("DAGGER_SESSION_PORT", "")
	t.Setenv("DAGGER_SESSION_TOKEN", "")
	_, err := moduleNames(t.Context(), "v1.0.0-beta.22")
	require.ErrorContains(t, err, "DAGGER_SESSION_PORT")
}
