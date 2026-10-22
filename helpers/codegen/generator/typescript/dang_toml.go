package typescriptgenerator

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// DangTomlFile is the dang.toml written beside the generated Dang entrypoint
// (entrypoint/main.dang). It declares the versions the entrypoint is written
// against: the Dang language version, and the Dagger API version through the
// Dagger import (vito/dang#185, #206, #208). An engine carrying
// dagger/dagger#14812 serves the module at that Dagger version — its API view
// and the naming rules its declarations get — instead of its own.
const DangTomlFile = "entrypoint/dang.toml"

const (
	// DangVersion is the Dang language version generated entrypoints are
	// written in.
	DangVersion = "2.1"

	// PreCutDaggerVersion is the Dagger version an entrypoint module that
	// predates its dang.toml is pinned to: what dagger/dagger#14812 serves a
	// versionless entrypoint at, the last view before the naming cut. Writing
	// it down keeps regeneration from moving the module onto the new naming.
	PreCutDaggerVersion = "v1.0.0-beta.20"

	// VersionedEntrypointsVersion is the first engine release that reads the
	// Dagger version out of an entrypoint's dang.toml (dagger/dagger#14812,
	// shipped with the naming cut). An older engine serves the entrypoint at its
	// own version whatever the file says, and its Dang may predate the
	// `dagger = "<version>"` form (vito/dang#206), so it gets no dang.toml.
	VersionedEntrypointsVersion = "v1.0.0-beta.21"
)

// RenderDangToml returns the dang.toml for a module's generated entrypoint,
// or "" when none should be written: the running engine predates versioned
// entrypoints, or reports a version that can't be compared ("devel"). It is
// only called for a module that has no dang.toml yet: an existing one is the
// module's, and is kept as it is.
//
// A freshly initialized module (fresh) is pinned to the running engine's
// version, minus build metadata; a module that already had an entrypoint is
// pinned to PreCutDaggerVersion, the version it was being served at.
func RenderDangToml(engineVersion string, fresh bool) string {
	version, ok := normalizeEngineVersion(engineVersion)
	if !ok || !atLeast(version, VersionedEntrypointsVersion) {
		return ""
	}
	dagger := PreCutDaggerVersion
	if fresh {
		dagger = version
	}
	return fmt.Sprintf(`# Versions the generated entrypoint beside this file is written against: the
# Dang language, and the Dagger API (and its naming rules) the engine serves the
# module at. Generation keeps this file as it is; raise dagger to adopt a newer
# API, then run `+"`dagger generate`"+`.
dang = %q

[imports.Dagger]
dagger = %q
`, DangVersion, dagger)
}

// normalizeEngineVersion returns an engine version with a "v" prefix and no
// build metadata, as dang.toml declares it.
func normalizeEngineVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return strings.TrimSuffix(v, semver.Build(v)), true
}

// atLeast reports whether engine version v is min or later. A dev build (a
// prerelease mentioning "dev") counts as its release, as the engine's own API
// views do.
func atLeast(v, minVersion string) bool {
	if pre := semver.Prerelease(v); strings.Contains(pre, "dev") {
		v = strings.TrimSuffix(v, pre)
	}
	return semver.Compare(v, minVersion) >= 0
}
