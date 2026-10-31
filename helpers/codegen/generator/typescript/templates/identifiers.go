package templates

import (
	"context"
	"fmt"
	"slices"

	"codegen/generator"
	"codegen/introspection"
)

// Identifiers are the engine's styling of the identifiers the bindings
// generate from schema names (Query.formatIdentifiers): an enum member's key,
// an enum's converter functions (<Enum>ValueToName), a field's options type
// (<Parent><Field>Opts), and the alias a generated file imports a module
// client under. TypeScript writes them in PascalCase; acronyms are in
// capitals, like the schema's own type names the same bindings expose
// (HTTPClient), so a member TCP is NetworkProtocol.TCP and asJson's options
// are FileAsJSONOpts. The engine styles them by the naming dictionary of the
// schema's version, and nothing here predicts one.
//
// Every name the schema gives is styled up front, in one query
// (LoadIdentifiers). A name looked up that wasn't is styled directly. A
// failed answer fails generation.
type Identifiers struct {
	ctx     context.Context
	engine  generator.Engine
	version string
	pascal  map[string]string
}

// formatIdentifiersQuery styles names in PascalCase with acronyms in capitals.
const formatIdentifiersQuery = `query FormatIdentifiers($names: [String!]!, $version: String) {
  formatIdentifiers(names: $names, casing: PASCAL, acronyms: UPPERCASE, version: $version)
}`

// LoadIdentifiers styles, in one query through engine, every name of schema
// the bindings generate an identifier from: its enums and their members, the
// fields with optional arguments, and the file names of the modules it splits
// into their own clients. version is the
// schema's, whose naming dictionary the engine styles them by; empty leaves
// it to the engine's own. A nil schema styles each name as it is looked up.
func LoadIdentifiers(ctx context.Context, engine generator.Engine, version string, schema *introspection.Schema) (*Identifiers, error) {
	ids := &Identifiers{ctx: ctx, engine: engine, version: version, pascal: map[string]string{}}
	if schema == nil {
		return ids, nil
	}
	var names []string
	for _, t := range schema.Types {
		if t.Kind == introspection.TypeKindEnum {
			names = append(names, t.Name)
		}
		for _, v := range t.EnumValues {
			names = append(names, v.Name)
		}
		for _, f := range t.Fields {
			if slices.ContainsFunc(f.Args, func(a introspection.InputValue) bool { return a.IsOptional() }) {
				names = append(names, f.Name)
			}
		}
	}
	for _, module := range schema.DependencyNames() {
		names = append(names, typescriptTemplateFuncs{}.depFileName(module))
	}
	slices.Sort(names)
	if err := ids.format(slices.Compact(names)); err != nil {
		return nil, err
	}
	return ids, nil
}

// format asks the engine to style names, and records its answers.
func (ids *Identifiers) format(names []string) error {
	if len(names) == 0 {
		return nil
	}
	if ids == nil || ids.engine == nil {
		return fmt.Errorf("identifiers: no engine to style %q with", names[0])
	}
	vars := map[string]any{"names": names, "version": nil}
	if ids.version != "" {
		vars["version"] = ids.version
	}
	var resp struct {
		FormatIdentifiers []string `json:"formatIdentifiers"`
	}
	if err := ids.engine.Query(ids.ctx, formatIdentifiersQuery, vars, &resp); err != nil {
		return fmt.Errorf("identifiers: %w", err)
	}
	if len(resp.FormatIdentifiers) != len(names) {
		return fmt.Errorf("identifiers: %d answers for %d names", len(resp.FormatIdentifiers), len(names))
	}
	for i, name := range names {
		ids.pascal[name] = resp.FormatIdentifiers[i]
	}
	return nil
}

// Pascal is the engine's PascalCase styling of name, asked for directly when
// LoadIdentifiers didn't.
func (ids *Identifiers) Pascal(name string) (string, error) {
	if ids != nil {
		if styled, ok := ids.pascal[name]; ok {
			return styled, nil
		}
	}
	if err := ids.format([]string{name}); err != nil {
		return "", err
	}
	return ids.pascal[name], nil
}
