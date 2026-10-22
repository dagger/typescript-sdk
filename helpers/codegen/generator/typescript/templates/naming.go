package templates

import (
	"strings"
	"unicode"

	"codegen/generator"
	"codegen/introspection"
)

// TypeScript identifiers for schema names.
//
// When the schema has Query.formatIdentifiers (schema views at
// v1.0.0-beta.21 and above), names are the schema's names as the engine's
// naming rules format them:
//
//   - methods, args and opts keys: CAMEL / UPPERCASE (httpClient, withGPU);
//   - "Opts" types and enum converters: PASCAL / UPPERCASE (ClientHTTPOpts);
//   - enum members: PASCAL / CAPITALIZED, the closest match to what the
//     generator has always written (Tcp, ObjectKind).
//
// Otherwise each name falls back to the legacy converter, so older schemas
// generate byte-identical code.
//
// Only TypeScript identifiers change. Everything sent over the wire (field
// names in select, arg keys, input object fields, enum values) and every type
// name stays exactly as the schema has it: the runtime looks generated classes
// up by their GraphQL type name (the loader, __loadCoreObject), and type names
// in views with Query.formatIdentifiers are already canonical anyway.
//
// Where a renamed identifier is part of the API a caller writes, the legacy
// spelling stays as a deprecated alias: methods, top-level module functions,
// Opts types, opts keys and enum members.

var (
	camelNames             = introspection.NameFormat{Casing: introspection.CasingCamel, Acronyms: introspection.AcronymsUppercase}
	pascalNames            = introspection.NameFormat{Casing: introspection.CasingPascal, Acronyms: introspection.AcronymsUppercase}
	pascalCapitalizedNames = introspection.NameFormat{Casing: introspection.CasingPascal, Acronyms: introspection.AcronymsCapitalized}
)

// NameFormats are the formats TypeScript codegen names identifiers in: load
// them onto the schema with LoadFormattedNames before rendering.
var NameFormats = []introspection.NameFormat{camelNames, pascalNames, pascalCapitalizedNames}

// engineName returns a schema name as the engine formats it in f, if the
// schema has engine-formatted names. Names with a leading underscore
// (internal names, like _DirectiveApplication) keep the legacy conversion,
// which keeps the underscore.
func (funcs typescriptTemplateFuncs) engineName(name string, f introspection.NameFormat) (string, bool) {
	if strings.HasPrefix(name, "_") {
		return "", false
	}
	schema := funcs.fullSchema
	if schema == nil {
		schema = generator.GetSchema()
	}
	return schema.FormattedName(name, f)
}

// formatMethodName returns the TS name of the method generated for a field.
func (funcs typescriptTemplateFuncs) formatMethodName(name string) string {
	if formatted, ok := funcs.engineName(name, camelNames); ok {
		return funcs.formatName(formatted)
	}
	return funcs.formatName(name)
}

// legacyMethodName returns the TS name formatMethodName gave a field before
// the engine's naming rules, kept as a deprecated alias when it differs.
func (funcs typescriptTemplateFuncs) legacyMethodName(name string) string {
	return funcs.formatName(name)
}

// formatMemberName renders a schema field as a TypeScript member of a class
// field or a top-level function. Fields are camelCase by convention, but a
// module whose name starts with an underscore gets a capitalised root field,
// and the binding for it would then be the one member of the API not shaped
// like the rest, so the legacy rule lowers its first letter. The wire name is
// rendered separately from the raw field, so this changes what the caller
// writes, not what is selected.
func (funcs typescriptTemplateFuncs) formatMemberName(s string) string {
	if formatted, ok := funcs.engineName(s, camelNames); ok {
		return funcs.formatName(formatted)
	}
	return funcs.legacyMemberName(s)
}

// legacyMemberName returns the TS name formatMemberName gave a field before
// the engine's naming rules, kept as a deprecated alias when it differs.
func (funcs typescriptTemplateFuncs) legacyMemberName(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return funcs.formatName(string(runes))
}

// argName returns the TS name of an argument, unescaped: it is the key of the
// argument in the method's Opts type.
func (funcs typescriptTemplateFuncs) argName(name string) string {
	if formatted, ok := funcs.engineName(name, camelNames); ok {
		return formatted
	}
	return name
}

// formatArgName returns the TS name of an argument used as a parameter,
// escaped against reserved words.
func (funcs typescriptTemplateFuncs) formatArgName(name string) string {
	return funcs.formatName(funcs.argName(name))
}

// pascalCase converts a schema name into a PascalCase identifier part, for
// the names the generator derives from schema names: "<Parent><Field>Opts"
// types and the "<Enum>ValueToName" / "<Enum>NameToValue" converters.
func (funcs typescriptTemplateFuncs) pascalCase(name string) string {
	if formatted, ok := funcs.engineName(name, pascalNames); ok {
		return formatted
	}
	return toPascalCase(name)
}

// optsTypeName returns the name of a field's Opts type; parent is the TS name
// of the parent type (Client for Query).
func (funcs typescriptTemplateFuncs) optsTypeName(parent, field string) string {
	return parent + funcs.pascalCase(field) + "Opts"
}

// legacyOptsTypeName returns the name the generator gave a field's Opts type
// before the engine's naming rules, kept as a deprecated alias when it
// differs.
func (funcs typescriptTemplateFuncs) legacyOptsTypeName(parent, field string) string {
	return parent + toPascalCase(field) + "Opts"
}

// formatEnum returns the TS name of an enum member. It uses the same
// PascalCase rule as the rest of the generator rather than strcase.ToCamel,
// which mangles consecutive capitals ("EStarGZ" -> "EstarGz" instead of
// "EStarGz") and would leave a module's bindings incompatible with code
// written against the engine's.
func (funcs typescriptTemplateFuncs) formatEnum(name string) string {
	if formatted, ok := funcs.engineName(name, pascalCapitalizedNames); ok {
		return formatted
	}
	return toPascalCase(name)
}

// legacyEnumAlias returns the name the generator gave an enum member before
// the engine's naming rules (GITHUB_SSH was GithubSsh, is GitHubSsh), to keep
// as a deprecated alias of the member, or "" when it is the same name or the
// name of another member of the enum.
func (funcs typescriptTemplateFuncs) legacyEnumAlias(enum *introspection.Type, name string) string {
	legacy := toPascalCase(name)
	if legacy == funcs.formatEnum(name) {
		return ""
	}
	for _, v := range enum.EnumValues {
		if funcs.formatEnum(v.Name) == legacy {
			return ""
		}
	}
	return legacy
}

// renamedArgs returns the args whose TS name differs from their schema name.
// Their Opts keys are mapped back to the schema name before the call.
func (funcs typescriptTemplateFuncs) renamedArgs(values introspection.InputValues) introspection.InputValues {
	var renamed introspection.InputValues
	for _, v := range values {
		if funcs.argName(v.Name) != v.Name {
			renamed = append(renamed, v)
		}
	}
	return renamed
}

// methodAlias is the dot of the "method_alias" template: a deprecated alias
// for a method renamed by the engine's naming rules.
type methodAlias struct {
	// Class is the TS name of the class (or interface) declaring the method.
	Class string
	// Name is the method's TS name; Legacy the name it had before.
	Name, Legacy string
}

// methodAliasFor returns the deprecated alias a field's method needs, or nil
// when its name didn't change. member selects the member-name rule the method
// is rendered with (FormatMemberName rather than FormatMethodName).
func (funcs typescriptTemplateFuncs) methodAliasFor(field *introspection.Field, member bool) *methodAlias {
	name, legacy := funcs.formatMethodName(field.Name), funcs.legacyMethodName(field.Name)
	if member {
		name, legacy = funcs.formatMemberName(field.Name), funcs.legacyMemberName(field.Name)
	}
	if name == legacy || field.ParentObject == nil {
		return nil
	}
	return &methodAlias{
		Class:  funcs.formatName(funcs.queryToClient(field.ParentObject.Name)),
		Name:   name,
		Legacy: legacy,
	}
}

// optsFields is the dot of the "field" template: the fields of an Opts or
// input type. Opts keys are TS identifiers, mapped back to the schema name in
// the method body; input object fields go over the wire as they are.
type optsFields struct {
	Fields introspection.InputValues
	Opts   bool
}

func optsFieldList(fields introspection.InputValues) optsFields {
	return optsFields{Fields: fields, Opts: true}
}

func inputFieldList(fields introspection.InputValues) optsFields {
	return optsFields{Fields: fields}
}
