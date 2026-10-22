package naming

// This package is a verbatim copy of github.com/dagger/dagger's engine/naming
// (minus schema_test.go, which reads the engine's own GraphQL schema), so the
// engine-free generator names identifiers exactly as the engine's
// Query.formatIdentifiers does. testdata/vectors.json is upstream's shared
// vector file: TestSharedVectors failing after a re-vendor means this copy and
// the file disagree, and TestFormatIdentifiersParity (in introspection) checks
// it against a live engine when one is reachable.
//
// Re-vendor when the engine gains a dictionary (a new entry in releases, in
// version.go): copy engine/naming over this directory, drop schema_test.go,
// and update UpstreamCommit.

// UpstreamCommit is the dagger/dagger commit this copy was taken from.
const UpstreamCommit = "198d001192d3179a8215980e97c766026f2dd0c2"
