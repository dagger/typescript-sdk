package introspection

import (
	"fmt"
	"sort"
	"strings"

	"codegen/naming"
)

// Codegen names SDK identifiers with the engine's naming rules: the names
// Query.formatIdentifiers would give, computed by the vendored copy of the
// engine's engine/naming package (see naming/upstream.go). This generator runs
// engine-free, from introspection JSON, so it can't ask the engine; the copy,
// its shared test vectors and TestFormatIdentifiersParity are what keep the
// two in step.
//
// The gate is the schema being generated: Query.formatIdentifiers is only
// visible in views from v1.0.0-beta.21, the same views whose module
// declarations the engine names with engine/naming. A schema without it
// formats nothing, and codegen keeps its legacy converters, so older schemas
// generate byte-identical code.

// Casing is a convention for joining words into an identifier: a value of the
// engine's Casing enum.
type Casing string

const (
	CasingPascal         Casing = "PASCAL"          // HTTPClient
	CasingCamel          Casing = "CAMEL"           // httpClient
	CasingSnake          Casing = "SNAKE"           // http_client
	CasingScreamingSnake Casing = "SCREAMING_SNAKE" // HTTP_CLIENT
	CasingKebab          Casing = "KEBAB"           // http-client
	CasingFlat           Casing = "FLAT"            // httpclient
)

// AcronymStyle is how acronyms and terms are written where a word starts with
// a capital: a value of the engine's AcronymStyle enum.
type AcronymStyle string

const (
	AcronymsUppercase   AcronymStyle = "UPPERCASE"   // HTTPClient
	AcronymsCapitalized AcronymStyle = "CAPITALIZED" // HttpClient
)

// NameFormat is a casing with an acronym style. Its text form is
// "CASING:ACRONYMS", e.g. "SNAKE:UPPERCASE" or "PASCAL:CAPITALIZED".
type NameFormat struct {
	Casing   Casing
	Acronyms AcronymStyle
}

func (f NameFormat) String() string {
	return string(f.Casing) + ":" + string(f.Acronyms)
}

// HasFormatIdentifiers reports whether the schema has Query.formatIdentifiers,
// the gate for naming its identifiers with the engine's naming rules.
func (s *Schema) HasFormatIdentifiers() bool {
	if s == nil {
		return false
	}
	query := s.Query()
	if query == nil {
		return false
	}
	for _, f := range query.Fields {
		if f.Name == "formatIdentifiers" {
			return true
		}
	}
	return false
}

// Names returns the distinct type, field, argument, input field and enum
// value names in the schema, sorted. It leaves out introspection names ("__"
// prefix, including every name inside an introspection type) and names the
// engine can't format (no letters or digits, like "_").
func (s *Schema) Names() []string {
	seen := map[string]bool{}
	add := func(name string) {
		if strings.HasPrefix(name, "__") || !formattable(name) {
			return
		}
		seen[name] = true
	}
	for _, t := range s.Types {
		if strings.HasPrefix(t.Name, "__") {
			continue
		}
		add(t.Name)
		for _, f := range t.Fields {
			add(f.Name)
			for _, arg := range f.Args {
				add(arg.Name)
			}
		}
		for _, f := range t.InputFields {
			add(f.Name)
		}
		for _, v := range t.EnumValues {
			add(v.Name)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formattable reports whether the engine can parse name: ASCII, with at least
// one letter or digit.
func formattable(name string) bool {
	alnum := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 0x80 {
			return false
		}
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
			alnum = true
		}
	}
	return alnum
}

// FormatNames formats names in f the way Query.formatIdentifiers(names,
// casing, acronyms, version: schemaVersion) does: parsed with the naming
// dictionary of schemaVersion, the __schemaVersion of the schema being
// generated. Names the engine can't parse are left out of the map.
func FormatNames(names []string, f NameFormat, schemaVersion string) (map[string]string, error) {
	casing, style, err := f.naming()
	if err != nil {
		return nil, err
	}
	dict := naming.DictionaryFor(schemaVersion)
	formatted := make(map[string]string, len(names))
	for _, name := range names {
		if _, done := formatted[name]; done || !formattable(name) {
			continue
		}
		id, err := dict.Parse(name)
		if err != nil {
			continue
		}
		formatted[name] = id.Format(casing, style)
	}
	return formatted, nil
}

// LoadFormattedNames formats every name of the schema (see Names) in each of
// formats, with the naming dictionary of schemaVersion (see FormatNames), and
// keeps the results on the schema for FormattedName. A schema without
// Query.formatIdentifiers is left as is, so FormattedName reports false and
// codegen falls back to its legacy converters.
func (s *Schema) LoadFormattedNames(schemaVersion string, formats ...NameFormat) error {
	if !s.HasFormatIdentifiers() {
		return nil
	}
	names := s.Names()
	for _, f := range formats {
		if _, ok := s.FormattedNames[f]; ok {
			continue
		}
		formatted, err := FormatNames(names, f, schemaVersion)
		if err != nil {
			return err
		}
		if s.FormattedNames == nil {
			s.FormattedNames = map[NameFormat]map[string]string{}
		}
		s.FormattedNames[f] = formatted
	}
	return nil
}

// FormattedName returns a schema name as the engine formats it in f. It
// reports false when the name wasn't formatted in f: the schema predates
// Query.formatIdentifiers, f wasn't loaded, or the name isn't one of the
// schema's names (see Names). Codegen then uses its legacy converter.
func (s *Schema) FormattedName(name string, f NameFormat) (string, bool) {
	if s == nil {
		return "", false
	}
	formatted, ok := s.FormattedNames[f][name]
	return formatted, ok
}

func (f NameFormat) naming() (naming.Casing, naming.AcronymStyle, error) {
	for _, casing := range naming.Casings {
		if casing.String() != string(f.Casing) {
			continue
		}
		for _, style := range naming.AcronymStyles {
			if style.String() == string(f.Acronyms) {
				return casing, style, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("unknown name format %s", f)
}
