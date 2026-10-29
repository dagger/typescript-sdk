// Package identifiers names identifiers by the engine's identifier naming
// rules, through the engine's own Query.formatIdentifiers, for code generated
// for versions at or past CasingVersion.
//
// Copied from github.com/dagger/go-sdk, cmd/dagger-go-sdk-codegen/identifiers
// (dagger/go-sdk#90, 73afbab), which is the pattern every SDK port follows.
// What differs is the import paths, the set of formats (TypeScript styles no
// identifiers of its own, so there is no GoName) and the comments saying so;
// the gate, the batching and the session client are the same code, and are to
// be kept in step rather than left to drift. A copy rather than an import:
// this codegen is a self-contained module ("codegen"), and depending on the Go
// SDK's generator for 300 lines would tie the two repositories' releases.
//
// From CasingVersion on, the engine parses a name into words with a versioned
// dictionary of acronyms and terms (engine/naming in dagger/dagger), both to
// name a module's declarations and to answer formatIdentifiers. A generator
// that formats names itself, or with another copy of those rules, eventually
// disagrees with the engine, so generators ask the engine instead:
//
//   - Gate: Connector.Names returns nil, the legacy rules, unless both the
//     version the names are for and the engine's release are at or past
//     CasingVersion. Older engines name every module by strcase, and older
//     versions keep the names they shipped with, so nil means the generator
//     produces exactly what it did before the rules existed. Nothing gates on
//     whether formatIdentifiers is in the schema: engines serve it on every
//     v1 view, including the views whose modules keep the legacy rules.
//   - Batching: a generator looks names up while it renders. A name that isn't
//     formatted yet is recorded, and Names.Run formats every recorded name in
//     a single GraphQL query (one aliased formatIdentifiers field per Format),
//     then renders again, until nothing is missing.
//   - Versioning: every request passes the version its Names is for (a
//     module's effective version), so names are parsed with that version's
//     dictionary whatever version the session is served at. Released
//     dictionaries never change, so the output is stable.
package identifiers

import (
	"strings"

	"golang.org/x/mod/semver"
)

// CasingVersion is engine.IdentifierCasingVersion in dagger/dagger: the first
// engine release, and API view, that names module declarations with its
// identifier naming rules (and whose core API follows the GraphQL casing
// convention). Released versions never move it.
const CasingVersion = "v1.0.0-beta.22"

// Casing is a value of the engine's Casing enum: how words are joined.
type Casing string

const (
	Pascal         Casing = "PASCAL"
	Camel          Casing = "CAMEL"
	ScreamingSnake Casing = "SCREAMING_SNAKE"
	Kebab          Casing = "KEBAB"
)

// AcronymStyle is a value of the engine's AcronymStyle enum: how acronyms and
// terms are written where a word starts with a capital.
type AcronymStyle string

const (
	Uppercase   AcronymStyle = "UPPERCASE"
	Capitalized AcronymStyle = "CAPITALIZED"
)

// Format is how formatIdentifiers formats a name.
type Format struct {
	Casing   Casing
	Acronyms AcronymStyle
}

// The formats the TypeScript generators use: the schema names the engine
// gives a module's own declarations (core.Namer in dagger/dagger), which the
// generated entrypoints have to predict.
var (
	// TypeName is the schema name the engine gives a module's object,
	// interface or enum (MyModHTTPClient).
	TypeName = Format{Pascal, Uppercase}
	// FieldName is the schema name the engine gives a module's field,
	// function or argument (baseUrl, httpClient).
	FieldName = Format{Camel, Capitalized}
	// EnumMemberName is the schema name the engine gives a module's enum
	// member that isn't already conventional (HTTP_CLIENT).
	EnumMemberName = Format{ScreamingSnake, Uppercase}
	// Words is a name's words, lowercase and joined by "-" (http-client):
	// how a generator tells where the engine cuts a name into words.
	Words = Format{Kebab, Uppercase}
)

// maxLength is the longest name, in bytes, the engine parses.
const maxLength = 256

// Valid reports whether the engine can parse name: at most 256 bytes, ASCII,
// with at least one letter or digit. formatIdentifiers fails the whole batch
// on a name it can't parse, so such names are never sent; a generator keeps
// its fallback for them, as the engine keeps such a name as it is.
func Valid(name string) bool {
	if len(name) > maxLength {
		return false
	}
	alnum := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 0x80:
			return false
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
			alnum = true
		}
	}
	return alnum
}

// Applies reports whether a version is at or past CasingVersion. The version
// is an engine release (Query.version) or a module's engine version. Build
// metadata is dropped, and a development build counts as its release
// (v1.0.0-dev-<commit> is v1.0.0), as the engine maps versions to API views;
// other prereleases order as semver says, so every v1.0.0 beta before
// CasingVersion keeps the legacy rules. An invalid version doesn't apply.
func Applies(version string) bool {
	v := release(version)
	return semver.IsValid(v) && semver.Compare(v, CasingVersion) >= 0
}

// release normalizes a version the way the engine maps it to an API view.
func release(version string) string {
	v := strings.TrimSpace(version)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	v = strings.TrimSuffix(v, semver.Build(v))
	if pre := semver.Prerelease(v); strings.Contains(pre, "dev") {
		v = strings.TrimSuffix(v, pre)
	}
	return v
}
