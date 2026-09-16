package templates

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"golang.org/x/mod/semver"

	"github.com/iancoleman/strcase"

	"codegen/generator"
	"codegen/introspection"
)

func TypescriptTemplateFuncs(
	schemaVersion string,
	fullSchema *introspection.Schema,
	selfModule string,
	cfg generator.Config,
) template.FuncMap {
	return typescriptTemplateFuncs{
		cfg:           cfg,
		schemaVersion: schemaVersion,
		fullSchema:    fullSchema,
		selfModule:    selfModule,
	}.FuncMap()
}

type typescriptTemplateFuncs struct {
	schemaVersion string
	cfg           generator.Config

	// fullSchema is the complete, unfiltered schema (all dependency types
	// included). The per-file render data may carry a filtered schema (the
	// core schema for client.gen.ts, or a single dep's schema), so the
	// dependency-splitting helpers consult fullSchema to enumerate deps and
	// to decide which types belong to client.gen.ts vs. a per-dep file.
	fullSchema *introspection.Schema

	// selfModule is the name of the module the client is being generated for,
	// excluded from the module enumeration when set. Every current caller
	// passes "" — a module's own API splits into its own file like any
	// dependency's.
	selfModule string
}

// DependencyModules returns the schema's dependency module names with the
// module being generated for (self) removed: only dependencies are split into
// their own files. Names are compared kebab-cased to tolerate casing/separator
// differences between sourceMap module names and the configured name. This is
// the single source of truth shared by the generator (which filters the schema)
// and the templates (which enumerate the per-dep files).
func DependencyModules(schema *introspection.Schema, self string) []string {
	if schema == nil {
		return nil
	}
	all := schema.DependencyNames()
	out := make([]string, 0, len(all))
	for _, name := range all {
		if isSameModule(name, self) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// dependencyNames returns the modules whose types are split into their own
// files: every module in the schema except the one being generated for.
func (funcs typescriptTemplateFuncs) dependencyNames() []string {
	return DependencyModules(funcs.fullSchema, funcs.selfModule)
}

// isSameModule compares two module names tolerant of casing/separator
// differences (sourceMap module names vs. the configured module name).
func isSameModule(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strcase.ToKebab(a) == strcase.ToKebab(b)
}

func (funcs typescriptTemplateFuncs) FuncMap() template.FuncMap {
	formatTypeFunc := &FormatTypeFunc{
		formatNameFunc: funcs.formatName,
	}
	commonFunc := generator.NewCommonFunctions(funcs.schemaVersion, formatTypeFunc)
	return template.FuncMap{
		"FormatFieldOutputType":     funcs.formatFieldOutputType(commonFunc),
		"FormatFieldReturnType":     funcs.formatFieldReturnType(commonFunc),
		"CommentToLines":            funcs.commentToLines,
		"FormatDeprecation":         funcs.formatDeprecation,
		"FormatExperimental":        funcs.formatExperimental,
		"FormatReturnType":          commonFunc.FormatReturnType,
		"FormatInputType":           funcs.formatInputType(commonFunc),
		"FormatOutputType":          commonFunc.FormatOutputType,
		"FormatEnum":                funcs.formatEnum,
		"FormatName":                funcs.formatName,
		"FormatMemberName":          funcs.formatMemberName,
		"QueryToClient":             funcs.queryToClient,
		"GetOptionalArgs":           funcs.getOptionalArgs,
		"GetRequiredArgs":           funcs.getRequiredArgs,
		"HasPrefix":                 strings.HasPrefix,
		"PascalCase":                funcs.pascalCase,
		"IsArgOptional":             funcs.isArgOptional,
		"IsCustomScalar":            funcs.isCustomScalar,
		"IsEnum":                    funcs.isEnum,
		"IsKeyword":                 funcs.isKeyword,
		"ArgsHaveDescription":       funcs.argsHaveDescription,
		"SortInputFields":           funcs.sortInputFields,
		"SortEnumFields":            funcs.sortEnumFields,
		"ExtractEnumValue":          funcs.extractEnumValue,
		"GroupEnumByValue":          funcs.groupEnumByValue,
		"GetInputEnumValueType":     funcs.getInputEnumValueType,
		"Solve":                     funcs.solve,
		"Subtract":                  funcs.subtract,
		"ConvertID":                 commonFunc.ConvertID,
		"IsSelfChainable":           commonFunc.IsSelfChainable,
		"IsListOfObject":            commonFunc.IsListOfObject,
		"IsListOfInterface":         funcs.isListOfInterface,
		"IsNullableObject":          funcs.isNullableObject,
		"IsListOfEnum":              commonFunc.IsListOfEnum,
		"GetArrayField":             commonFunc.GetArrayField,
		"ToLowerCase":               commonFunc.ToLowerCase,
		"ToUpperCase":               commonFunc.ToUpperCase,
		"ToSingleType":              funcs.toSingleType,
		"GetEnumValues":             funcs.getEnumValues,
		"IsInterface":               funcs.isInterface,
		"CheckVersionCompatibility": commonFunc.CheckVersionCompatibility,
		"ModuleRelPath":             funcs.moduleRelPath,
		"FormatProtected":           funcs.formatProtected,
		"IsBundle":                  funcs.isBundle,
		"LegacyTypeScriptSDKCompat": funcs.legacyTypeScriptSDKCompat,
		"LegacyIDableTypes":         funcs.legacyIDableTypes,
		"LegacyIDName":              funcs.legacyIDName,
		"LegacyLoadFromIDName":      funcs.legacyLoadFromIDName,
		// Module splitting: render each module's types into its own
		// <module>.gen.ts client file, and the entrypoint loader that maps a
		// type name to the generated class whichever file it lives in.
		"DependencyFiles":     funcs.dependencyFiles,
		"DepFileName":         funcs.depFileName,
		"CoreFile":            funcs.coreFile,
		"ClientImports":       funcs.clientImports,
		"ClientRuntimeImport": funcs.clientRuntimeImport,
		"LibraryImport":       funcs.libraryImportSpec,
		"LoaderFiles":         funcs.loaderFiles,
		"RootClientType":      funcs.rootClientType,
		"IsExtendableType":    funcs.isExtendableType,
		// Serve-on-use: a module client serves its own module before its first
		// query, so a client used outside the dispatcher still resolves.
		"JSString":      jsString,
		"IsGitModule":   func(kind string) bool { return kind == generator.ModuleKindGit },
		"WorkspacePath": workspaceServePath,
	}
}

// workspaceServePath normalizes a local module's path to the workspace-root
// absolute form the currentWorkspace serve resolves from, cwd-independent —
// matching the dispatcher's own local serve.
func workspaceServePath(path string) string {
	return "/" + strings.TrimPrefix(strings.TrimPrefix(path, "./"), "/")
}

// legacyTypeScriptSDKCompatCutoverVersion is the first engine version whose
// TypeScript SDK surface is generated with unified ID-only re-entry and
// first-class interface client types. The -0 prerelease floor intentionally
// treats v0.21.0 dev builds as post-cutover while keeping v0.20.x modules in
// legacy mode.
const legacyTypeScriptSDKCompatCutoverVersion = "v0.21.0-0"

func (funcs typescriptTemplateFuncs) legacyTypeScriptSDKCompat() bool {
	if funcs.schemaVersion == "" || !semver.IsValid(funcs.schemaVersion) {
		return false
	}
	return semver.Compare(funcs.schemaVersion, legacyTypeScriptSDKCompatCutoverVersion) < 0
}

func (funcs typescriptTemplateFuncs) supportsNullableObjects() bool {
	return generator.SupportsNullableObjects(funcs.schemaVersion)
}

// isInterface checks if the type is a GraphQL interface.
func (funcs typescriptTemplateFuncs) isInterface(t *introspection.Type) bool {
	return t.Kind == introspection.TypeKindInterface
}

// formatInputType returns a function that formats input values.
func (funcs typescriptTemplateFuncs) formatInputType(
	commonFunc *generator.CommonFunctions,
) func(arg introspection.InputValue, scopes ...string) (string, error) {
	return func(arg introspection.InputValue, scopes ...string) (string, error) {
		if expectedType := arg.Directives.ExpectedType(); expectedType != "" {
			if arg.Name == "id" && funcs.legacyTypeScriptSDKCompat() && expectedType != "Node" && !strings.HasPrefix(expectedType, "_") {
				representation := funcs.scoped(scopes...) + funcs.legacyIDName(expectedType)
				if arg.TypeRef != nil && arg.TypeRef.IsList() {
					representation += "[]"
				}
				return representation, nil
			}
			representation := funcs.scoped(scopes...) + funcs.formatName(expectedType)
			if arg.TypeRef != nil && arg.TypeRef.IsList() {
				representation += "[]"
			}
			return representation, nil
		}
		if arg.Name == "id" {
			return commonFunc.FormatOutputType(arg.TypeRef, scopes...)
		}
		return commonFunc.FormatInputType(arg.TypeRef, scopes...)
	}
}

func (funcs typescriptTemplateFuncs) isListOfInterface(t *introspection.TypeRef) bool {
	if t == nil || !t.IsList() {
		return false
	}
	for ref := t; ref != nil; ref = ref.OfType {
		switch ref.Kind {
		case introspection.TypeKindNonNull, introspection.TypeKindList:
			continue
		default:
			return ref.Kind == introspection.TypeKindInterface
		}
	}
	return false
}

// formatFieldOutputType returns the raw response value type for a field. Legacy
// ID fields use old FooID aliases so generated caches/constructors match the
// pre-cutover public surface.
func (funcs typescriptTemplateFuncs) formatFieldOutputType(
	commonFunc *generator.CommonFunctions,
) func(field introspection.Field, scopes ...string) (string, error) {
	return func(field introspection.Field, scopes ...string) (string, error) {
		if funcs.legacyTypeScriptSDKCompat() && field.TypeRef.IsScalar() {
			if expectedType := funcs.fieldExpectedIDType(field); expectedType != "" {
				return funcs.scoped(scopes...) + funcs.legacyIDName(expectedType), nil
			}
		}
		return commonFunc.FormatOutputType(field.TypeRef, scopes...)
	}
}

// formatFieldReturnType returns the public method return type for a field. ID
// fields that are object re-entry points remain converted to their object type;
// plain ID fields use legacy FooID aliases only in legacy mode.
func (funcs typescriptTemplateFuncs) formatFieldReturnType(
	commonFunc *generator.CommonFunctions,
) func(field introspection.Field, scopes ...string) (string, error) {
	return func(field introspection.Field, scopes ...string) (string, error) {
		if commonFunc.ConvertID(field) {
			return commonFunc.FormatReturnType(field, scopes...)
		}
		if funcs.legacyTypeScriptSDKCompat() && field.TypeRef.IsScalar() {
			if expectedType := funcs.fieldExpectedIDType(field); expectedType != "" {
				return funcs.scoped(scopes...) + funcs.legacyIDName(expectedType), nil
			}
		}
		return commonFunc.FormatReturnType(field, scopes...)
	}
}

func (funcs typescriptTemplateFuncs) scoped(scopes ...string) string {
	scope := strings.Join(scopes, "")
	if scope != "" {
		scope += "."
	}
	return scope
}

func (funcs typescriptTemplateFuncs) fieldExpectedIDType(field introspection.Field) string {
	if !field.TypeRef.IsScalar() {
		return ""
	}
	ref := field.TypeRef
	if ref.Kind == introspection.TypeKindNonNull {
		ref = ref.OfType
	}
	if ref.Kind != introspection.TypeKindScalar || ref.Name != "ID" {
		return ""
	}
	if expectedType := field.Directives.ExpectedType(); expectedType != "" && expectedType != "Node" && !strings.HasPrefix(expectedType, "_") {
		return expectedType
	}
	if field.Name == "id" && field.ParentObject != nil && field.ParentObject.Name != "Node" && !strings.HasPrefix(field.ParentObject.Name, "_") {
		return field.ParentObject.Name
	}
	return ""
}

// pascalCase converts a type name to PascalCase. strcase.ToCamel mishandles
// runs of consecutive uppercase letters (e.g. "LLMContentBlockKind" ->
// "LlmcontentBlockKind"), which would make the generated enum converter
// definitions (named via PascalCase) diverge from their raw-named call sites
// and fail to type-check, so use a custom implementation that preserves
// configured acronyms.
func (funcs typescriptTemplateFuncs) pascalCase(name string) string {
	return toPascalCase(name)
}

var (
	reUpperToUpperLower = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	reLowerToUpper      = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// pascalCaseAcronyms lists tokens that render in a fixed canonical form instead
// of being title-cased, so acronym names round-trip consistently: the "llm"
// field and the schema's LLM* types both render as "LLM" (mirroring the Go
// SDK's strcase.ConfigureAcronym("LLM", "LLM")). Keyed by the uppercased token.
var pascalCaseAcronyms = map[string]string{
	"LLM": "LLM",
}

func toPascalCase(s string) string {
	// Insert word boundaries: "LLMContent" -> "LLM_Content", "blockKind" -> "block_Kind"
	s = reUpperToUpperLower.ReplaceAllString(s, `${1}_${2}`)
	s = reLowerToUpper.ReplaceAllString(s, `${1}_${2}`)

	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		if acr, ok := pascalCaseAcronyms[strings.ToUpper(p)]; ok {
			parts[i] = acr
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "")
}

// solve checks if a field is solvable.
func (funcs typescriptTemplateFuncs) solve(field introspection.Field) bool {
	if field.TypeRef == nil {
		return false
	}
	return field.TypeRef.IsScalar() || field.TypeRef.IsList() || funcs.isNullableObject(field.TypeRef)
}

func (funcs typescriptTemplateFuncs) isNullableObject(ref *introspection.TypeRef) bool {
	return funcs.supportsNullableObjects() && ref != nil && ref.IsOptional() && (ref.IsObject() || ref.IsInterface())
}

// subtract subtract integer a with integer b.
func (funcs typescriptTemplateFuncs) subtract(a, b int) int {
	return a - b
}

// commentToLines split a string by line breaks to be used in comments
func (funcs typescriptTemplateFuncs) commentToLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}

	// Escape */ to prevent premature closure of JSDoc block comments
	s = strings.ReplaceAll(s, "*/", `*\/`)

	split := strings.Split(s, "\n")
	return split
}

// format the deprecation reason
// Example: `Replaced by @foo.` -> `// Replaced by Foo\n`
func (funcs typescriptTemplateFuncs) formatDeprecation(s string) []string {
	return funcs.formatHelper("deprecated", s)
}

func (funcs typescriptTemplateFuncs) formatExperimental(_ string) []string {
	return funcs.formatHelper("experimental", "")
}

func (funcs typescriptTemplateFuncs) formatHelper(name string, s string) []string {
	r := regexp.MustCompile("`[a-zA-Z0-9_]+`")
	matches := r.FindAllString(s, -1)
	for _, match := range matches {
		replacement := strings.TrimPrefix(match, "`")
		replacement = strings.TrimSuffix(replacement, "`")
		replacement = funcs.formatName(replacement)
		s = strings.ReplaceAll(s, match, replacement)
	}
	return funcs.commentToLines("@" + name + " " + s)
}

// isCustomScalar checks if the type is actually custom.
func (funcs typescriptTemplateFuncs) isCustomScalar(t *introspection.Type) bool {
	switch introspection.Scalar(t.Name) {
	case introspection.ScalarString, introspection.ScalarInt, introspection.ScalarFloat, introspection.ScalarBoolean:
		return false
	default:
		return t.Kind == introspection.TypeKindScalar
	}
}

// isEnum checks if the type is actually custom.
func (funcs typescriptTemplateFuncs) isEnum(t *introspection.Type) bool {
	return t.Kind == introspection.TypeKindEnum &&
		// We ignore the internal GraphQL enums
		!strings.HasPrefix(t.Name, "_")
}

func (funcs typescriptTemplateFuncs) isKeyword(s string) bool {
	_, isKeyword := jsKeywords[strings.ToLower(s)]

	return isKeyword
}

// formatName formats a GraphQL name (e.g. object, field, arg) into a TS
// equivalent, avoiding collisions with reserved words.
func (funcs typescriptTemplateFuncs) formatName(s string) string {
	if _, isKeyword := jsKeywords[strings.ToLower(s)]; isKeyword {
		// NB: this is case-insensitive; in JS, both function and Function cause
		// problems (one straight up doesn't parse, the other causes lint errors)
		return s + "_"
	}
	return s
}

// formatMemberName renders a schema field as a TypeScript member. Fields are
// camelCase by convention and this is a no-op for them, but a module whose name
// starts with an underscore gets a capitalised field, and the binding for it
// would then be the one member of the API not shaped like the rest. The wire
// name is rendered separately from the raw field, so lowering the first letter
// here changes what the caller writes, not what is selected.
func (funcs typescriptTemplateFuncs) formatMemberName(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return funcs.formatName(string(runes))
}

func (funcs typescriptTemplateFuncs) queryToClient(s string) string {
	if s == generator.QueryStructName {
		return generator.QueryStructClientName
	}
	return s
}

// all words to avoid collisions with, whether they're reserved or not
//
// in practice, many of these work just fine as e.g. method
// names, like 'export' and 'from'.
var jsKeywords = map[string]struct{}{
	"arguments": {},
	"await":     {},
	"break":     {},
	"case":      {},
	"catch":     {},
	"class":     {},
	"const":     {},
	"continue":  {},
	"debugger":  {},
	"default":   {},
	"delete":    {},
	"do":        {},
	"else":      {},
	"enum":      {},
	// "export":     {}, // containr.export
	"extends":    {},
	"false":      {},
	"finally":    {},
	"for":        {},
	"function":   {},
	"if":         {},
	"implements": {},
	"import":     {},
	"in":         {},
	"instanceof": {},
	"interface":  {},
	"new":        {},
	"null":       {},
	"package":    {},
	"private":    {},
	"protected":  {},
	"public":     {},
	"return":     {},
	"super":      {},
	"switch":     {},
	"this":       {},
	"throw":      {},
	"true":       {},
	"try":        {},
	"typeof":     {},
	"var":        {},
	"void":       {},
	"while":      {},
	// "with":        {},
	"yield":       {},
	"as":          {},
	"let":         {},
	"static":      {},
	"any":         {},
	"boolean":     {},
	"constructor": {},
	"declare":     {},
	// "get":         {},
	"module":  {},
	"require": {},
	"number":  {},
	"set":     {},
	"string":  {},
	"symbol":  {},
	"type":    {},
	// "from":        {}, // container.from
	// "of":        {},
	"async":     {},
	"namespace": {},
}

// formatEnum formats a GraphQL enum into a TS equivalent
// formatEnum names an enum member. It uses the same PascalCase rule as the rest
// of the generator rather than strcase.ToCamel, which mangles consecutive
// capitals ("EStarGZ" -> "EstarGz" instead of "EStarGz") and would leave a
// module's bindings incompatible with code written against the engine's.
func (funcs typescriptTemplateFuncs) formatEnum(s string) string {
	return toPascalCase(s)
}

// isArgOptional checks if some arg are optional.
// They are, if all of there InputValues are optional.
func (funcs typescriptTemplateFuncs) isArgOptional(values introspection.InputValues) bool {
	for _, v := range values {
		if !v.IsOptional() {
			return false
		}
	}
	return true
}

func (funcs typescriptTemplateFuncs) splitRequiredOptionalArgs(values introspection.InputValues) (required introspection.InputValues, optionals introspection.InputValues) {
	for i, v := range values {
		if !v.IsOptional() {
			continue
		}

		return values[:i], values[i:]
	}
	return values, nil
}

func (funcs typescriptTemplateFuncs) getEnumValues(values introspection.InputValues) introspection.InputValues {
	enums := introspection.InputValues{}

	for _, v := range values {
		if v.TypeRef != nil && v.TypeRef.Kind == introspection.TypeKindEnum {
			enums = append(enums, v)
		}

		// Check parent if the parent is an enum (for instance with TypeDefKind)
		if v.TypeRef.OfType != nil && v.TypeRef.OfType.Kind == introspection.TypeKindEnum {
			enums = append(enums, v)
		}
	}

	return enums
}

func (funcs typescriptTemplateFuncs) getInputEnumValueType(enum introspection.InputValue) string {
	if enum.TypeRef.OfType != nil && enum.TypeRef.OfType.Kind == introspection.TypeKindEnum {
		return enum.TypeRef.OfType.Name
	}

	return enum.TypeRef.Name
}

func (funcs typescriptTemplateFuncs) getRequiredArgs(values introspection.InputValues) introspection.InputValues {
	required, _ := funcs.splitRequiredOptionalArgs(values)
	return required
}

func (funcs typescriptTemplateFuncs) getOptionalArgs(values introspection.InputValues) introspection.InputValues {
	_, optional := funcs.splitRequiredOptionalArgs(values)
	return optional
}

func (funcs typescriptTemplateFuncs) sortInputFields(s []introspection.InputValue) []introspection.InputValue {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Name < s[j].Name
	})
	return s
}

func (funcs typescriptTemplateFuncs) sortEnumFields(s []introspection.EnumValue) []introspection.EnumValue {
	copy := slices.Clone(s)

	slices.SortStableFunc(copy, func(x, y introspection.EnumValue) int {
		return cmp.Compare(strcase.ToCamel(x.Name), strcase.ToCamel(y.Name))
	})

	copy = slices.CompactFunc(copy, func(x, y introspection.EnumValue) bool {
		return strcase.ToCamel(x.Name) == strcase.ToCamel(y.Name)
	})

	return copy
}

func (funcs typescriptTemplateFuncs) extractEnumValue(enum introspection.EnumValue) string {
	return enum.Directives.EnumValue()
}

// groupEnumByValue returns a list of lists of enums, grouped by similar enum value.
//
// Additionally, enum names within a single value are removed (which would
// result in duplicate codegen).
func (funcs typescriptTemplateFuncs) groupEnumByValue(s []introspection.EnumValue) [][]introspection.EnumValue {
	m := map[string][]introspection.EnumValue{}
	for _, v := range s {
		value := cmp.Or(v.Directives.EnumValue(), v.Name)
		if !slices.ContainsFunc(m[value], func(other introspection.EnumValue) bool {
			return strcase.ToCamel(v.Name) == strcase.ToCamel(other.Name)
		}) {
			m[value] = append(m[value], v)
		}
	}

	var result [][]introspection.EnumValue
	for _, v := range s {
		value := cmp.Or(v.Directives.EnumValue(), v.Name)
		if res, ok := m[value]; ok {
			result = append(result, res)
			delete(m, value)
		}
	}

	return result
}

func (funcs typescriptTemplateFuncs) argsHaveDescription(values introspection.InputValues) bool {
	for _, o := range values {
		if strings.TrimSpace(o.Description) != "" {
			return true
		}
	}

	return false
}

func (funcs typescriptTemplateFuncs) toSingleType(value string) string {
	return value[:len(value)-2]
}

// moduleRelPath rewrites a source-map filelink — which is relative to its
// module's root — to be relative to the generated file that mentions it, so the
// trailing `// <module> (<file>)` comment stays clickable. Module bindings live
// one level down in sdk/; a standalone client's live at its own root.
func (funcs typescriptTemplateFuncs) moduleRelPath(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	if funcs.cfg.ModuleConfig == nil {
		return path
	}

	return filepath.Join("..", path)
}

func (funcs typescriptTemplateFuncs) formatProtected(s string) string {
	return strings.TrimSuffix(s, "_")
}

func (funcs typescriptTemplateFuncs) legacyIDName(typeName string) string {
	return typeName + "ID"
}

func (funcs typescriptTemplateFuncs) legacyLoadFromIDName(typeName string) string {
	return funcs.formatName("load" + typeName + "FromID")
}

// legacyIDableTypes returns, for the file currently being rendered, the types
// whose <Name>ID alias must be declared (legacy mode only). It is scoped to the
// file's own types so each <dep>.gen.ts emits only its dependency's ID aliases:
// reading the global schema instead would re-emit every core <Name>ID in every
// dep file, duplicating client.gen.ts's aliases (TS2308 via `export *`).
func (funcs typescriptTemplateFuncs) legacyIDableTypes(fileTypes []*introspection.Type) []*introspection.Type {
	if !funcs.legacyTypeScriptSDKCompat() {
		return nil
	}
	schema := generator.GetSchema()
	if schema == nil {
		return nil
	}
	var types []*introspection.Type
	for _, t := range fileTypes {
		if t == nil || t.Name == "Node" || strings.HasPrefix(t.Name, "_") {
			continue
		}
		if t.Kind != introspection.TypeKindObject && t.Kind != introspection.TypeKindInterface {
			continue
		}
		idName := funcs.legacyIDName(t.Name)
		if schema.Types.Get(idName) != nil {
			continue
		}
		if !slices.ContainsFunc(t.Fields, func(field *introspection.Field) bool {
			return field.Name == "id" && field.TypeRef != nil && field.TypeRef.IsScalar()
		}) {
			continue
		}
		types = append(types, t)
	}
	return types
}

// isBundle reports whether the core file sits next to the bundled SDK library
// and must import it from ./core.js. That is any scope's client bindings — a
// module's or a standalone client's — since both vendor the library as sdk/.
// Only the library's own bindings (no ModuleConfig) import the runtime they
// ship with, by relative source path.
func (funcs typescriptTemplateFuncs) isBundle() bool {
	return funcs.cfg.ModuleConfig != nil
}

// depFileName converts a module name to the kebab-cased basename used for its
// generated file, e.g. "myDep" -> "my-dep" (file "my-dep.gen.ts").
func (funcs typescriptTemplateFuncs) depFileName(moduleName string) string {
	return strcase.ToKebab(moduleName)
}

// coreFile is the basename (no extension) of the core generated file the
// client files import the runtime classes from: always client.gen, the
// vendored library's bindings.
func (funcs typescriptTemplateFuncs) coreFile() string {
	return "client.gen"
}

// isExtendableType reports whether the type is one of the core extendable
// types (Query) whose module-contributed fields become each module file's own
// Client rather than part of the module's regular classes.
func (funcs typescriptTemplateFuncs) isExtendableType(t *introspection.Type) bool {
	if t == nil {
		return false
	}
	return slices.Contains(introspection.ExtendableTypes, t.Name)
}

// dependencyFiles returns the per-dep generated filenames (kebab-cased, no
// extension), sorted by dependency name.
func (funcs typescriptTemplateFuncs) dependencyFiles() []string {
	if funcs.fullSchema == nil {
		return nil
	}
	deps := funcs.dependencyNames()
	out := make([]string, len(deps))
	for i, d := range deps {
		out[i] = funcs.depFileName(d)
	}
	return out
}

// isBuiltinScalar reports whether name is a GraphQL builtin scalar that maps to
// a native TS type (string/number/float/boolean) and is therefore never
// imported or exported by name.
func isBuiltinScalar(name string) bool {
	switch introspection.Scalar(name) {
	case introspection.ScalarString, introspection.ScalarInt,
		introspection.ScalarFloat, introspection.ScalarBoolean:
		return true
	}
	return false
}

// isExportableType reports whether the generated client surfaces this type by
// name — i.e. whether it can appear in a per-dep file's import and in
// client.gen.ts's re-export. It is the single predicate shared by the importing
// side (coreTypeNames/coreValueNames) and the exporting side
// so the two can't drift. It excludes internal (_-prefixed) types, builtin
// scalars, and the extendable types (each module renders its own Client, so
// the type is never imported).
func (funcs typescriptTemplateFuncs) isExportableType(t *introspection.Type) bool {
	if t == nil || strings.HasPrefix(t.Name, "_") {
		return false
	}
	if slices.Contains(introspection.ExtendableTypes, t.Name) {
		return false
	}
	return !isBuiltinScalar(t.Name)
}

// collectReferencedNames returns every type name appearing in the dependency
// surface (field return types, argument types, and input fields).
//
// An object-typed argument or input field is encoded in the schema as an `ID`
// scalar carrying an `@expectedType(name: "X")` directive, and the renderer
// resolves it to `X` (formatInputType). So the referenced type is the expected
// type, not the raw `ID` the TypeRef names — miss it and an object passed only
// as an argument (e.g. a constructor's `ws: Workspace`) renders in the
// signature but is never imported.
func (funcs typescriptTemplateFuncs) collectReferencedNames(depTypes []*introspection.Type) map[string]struct{} {
	referenced := map[string]struct{}{}
	visit := func(ref *introspection.TypeRef) {
		for ; ref != nil; ref = ref.OfType {
			if ref.Name != "" {
				referenced[ref.Name] = struct{}{}
			}
		}
	}
	visitInput := func(directives introspection.Directives, ref *introspection.TypeRef) {
		if et := directives.ExpectedType(); et != "" {
			referenced[et] = struct{}{}
		}
		visit(ref)
	}
	for _, t := range depTypes {
		for _, f := range t.Fields {
			visit(f.TypeRef)
			for _, a := range f.Args {
				visitInput(a.Directives, a.TypeRef)
			}
		}
		for _, in := range t.InputFields {
			visitInput(in.Directives, in.TypeRef)
		}
	}
	return referenced
}

// addLegacyIDRefs adds, in legacy mode, the <Object>ID alias names referenced
// by the dependency surface. An object's `id` field (and id-typed args) render
// as the per-type alias <Object>ID rather than the generic ID scalar, so the
// dep file imports those aliases from client.gen.ts.
func (funcs typescriptTemplateFuncs) addLegacyIDRefs(depTypes []*introspection.Type, referenced map[string]struct{}) {
	if !funcs.legacyTypeScriptSDKCompat() {
		return
	}
	objectNames := map[string]struct{}{}
	for _, t := range depTypes {
		if t.Kind == introspection.TypeKindObject {
			objectNames[t.Name] = struct{}{}
		}
	}
	for name := range referenced {
		if t := funcs.fullSchema.Types.Get(name); t != nil && t.Kind == introspection.TypeKindObject {
			objectNames[name] = struct{}{}
		}
	}
	for name := range objectNames {
		idName := funcs.legacyIDName(name)
		if funcs.fullSchema.Types.Get(idName) != nil {
			referenced[idName] = struct{}{}
		}
	}
}

// typeOwner returns the module a type is contributed by, "" for core types.
func typeOwner(t *introspection.Type) string {
	if sm := t.Directives.SourceMap(); sm != nil {
		return sm.Module
	}
	return ""
}

// ClientImport groups the identifiers a per-module client file imports from
// one other generated file: the core file, or a sibling module's. Values are
// runtime imports — object classes the bodies construct (`new X(ctx)`) and the
// enum converters they call; a type-only import used as a value is a hard tsc
// error (TS1361) and, erased under ESM, a runtime ReferenceError. Types cover
// everything that only appears in signatures and are erased at compile time.
type ClientImport struct {
	// From is the specifier the names are imported from, e.g.
	// "@dagger.io/dagger" for the core library or "./my-dep.gen.js" for a
	// sibling module client.
	From   string
	Values []string
	Types  []string
}

// clientRuntimeImport is the specifier a client file imports Context and
// BaseClient from. A scope's client files (module or standalone) reach the
// runtime through the @dagger.io/dagger package (resolved to the vendored sdk/
// by a tsconfig/import-map alias); only the library's own bindings import the
// runtime by relative source path.
func (funcs typescriptTemplateFuncs) clientRuntimeImport() string {
	if funcs.cfg.ModuleConfig == nil {
		return "../common/context.js"
	}
	return funcs.libraryImportSpec()
}

// coreImportSpec is where a client file imports core types and values from: the
// @dagger.io/dagger package, aliased to the vendored library's sdk/ directory.
func (funcs typescriptTemplateFuncs) coreImportSpec() string {
	if funcs.cfg.ModuleConfig != nil {
		return funcs.libraryImportSpec()
	}
	return "./" + funcs.coreFile() + ".js"
}

// libraryImportSpec is how a client file names the library: always the package,
// never a path into it. A generated client is a real npm package, so it reaches
// its dependency the way any package does — what resolves the name is the
// install, not the rendering.
func (funcs typescriptTemplateFuncs) libraryImportSpec() string {
	return "@dagger.io/dagger"
}

// siblingImportSpec is where a client file imports another module's types from.
// A module client reaches a sibling through its own package specifier
// (@dagger.io/<module>) — the same name a user writes and the tsconfig/import-map
// alias resolves — so the clients are forward-compatible with being published
// one package per module. A standalone client keeps its siblings relative, since
// they share one package directory.
func (funcs typescriptTemplateFuncs) siblingImportSpec(owner string) string {
	if funcs.cfg.ModuleConfig != nil {
		return "@dagger.io/" + funcs.depFileName(owner)
	}
	return "./" + funcs.depFileName(owner) + ".gen.js"
}

// clientImports plans a per-module client file's imports of the types it does
// not own: each referenced type resolves to the specifier of the file that
// declares it — the core library for core types, a sibling <module>.gen file
// otherwise. selfName is the module the file is rendered for; its own types are
// declared locally and never imported. The import direction is strictly module
// file -> core, so no ESM cycle; a sibling value import is only ever
// dereferenced inside a method body, after both files have evaluated, so a
// mutual reference between two modules is safe too.
func (funcs typescriptTemplateFuncs) clientImports(fileTypes []*introspection.Type, selfName string) []ClientImport {
	if funcs.fullSchema == nil {
		return nil
	}

	referenced := funcs.collectReferencedNames(fileTypes)
	funcs.addLegacyIDRefs(fileTypes, referenced)

	coreSpec := funcs.coreImportSpec()
	type group struct {
		values map[string]struct{}
		types  map[string]struct{}
	}
	groups := map[string]*group{}
	groupFor := func(spec string) *group {
		g, ok := groups[spec]
		if !ok {
			g = &group{values: map[string]struct{}{}, types: map[string]struct{}{}}
			groups[spec] = g
		}
		return g
	}
	// ownerSpec resolves a type to the import specifier of the file that
	// declares it, or "" when the type is the rendered module's own and needs
	// no import.
	ownerSpec := func(t *introspection.Type) string {
		owner := typeOwner(t)
		switch {
		case owner == "":
			return coreSpec
		case isSameModule(owner, selfName):
			return ""
		default:
			return funcs.siblingImportSpec(owner)
		}
	}

	for name := range referenced {
		// The Float scalar is the one builtin that renders as a TS alias
		// (`float`) declared in the core file rather than a native type, so it
		// is imported from there.
		if introspection.Scalar(name) == introspection.ScalarFloat {
			groupFor(coreSpec).types["float"] = struct{}{}
			continue
		}
		t := funcs.fullSchema.Types.Get(name)
		if t == nil || !funcs.isExportableType(t) {
			continue
		}
		spec := ownerSpec(t)
		if spec == "" {
			continue
		}
		if t.Kind == introspection.TypeKindObject {
			// A value import covers both `new X(ctx)` in bodies and X used as
			// a signature type. A fieldless object never renders a class, so
			// there is nothing to import for it.
			if len(t.Fields) > 0 {
				groupFor(spec).values[funcs.exportedTypeName(t)] = struct{}{}
			}
			continue
		}
		groupFor(spec).types[funcs.exportedTypeName(t)] = struct{}{}
	}

	// Enum converters, imported only in the direction actually used
	// (NameToValue for enum return values, ValueToName for enum arguments) so
	// the import is never unused. A converter is declared beside its enum, in
	// the enum owner's file.
	addEnumConverters := func(ref *introspection.TypeRef, suffix string) {
		for ; ref != nil; ref = ref.OfType {
			if ref.Name == "" {
				continue
			}
			t := funcs.fullSchema.Types.Get(ref.Name)
			if t == nil || t.Kind != introspection.TypeKindEnum || !funcs.isExportableType(t) {
				continue
			}
			spec := ownerSpec(t)
			if spec == "" {
				continue
			}
			groupFor(spec).values[funcs.pascalCase(t.Name)+suffix] = struct{}{}
		}
	}
	for _, t := range fileTypes {
		for _, f := range t.Fields {
			addEnumConverters(f.TypeRef, "NameToValue")
			for _, a := range f.Args {
				addEnumConverters(a.TypeRef, "ValueToName")
			}
		}
	}

	specs := make([]string, 0, len(groups))
	for spec := range groups {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool {
		if (specs[i] == coreSpec) != (specs[j] == coreSpec) {
			return specs[i] == coreSpec
		}
		return specs[i] < specs[j]
	})

	out := make([]ClientImport, 0, len(specs))
	for _, spec := range specs {
		g := groups[spec]
		out = append(out, ClientImport{
			From:   spec,
			Values: sortedNames(g.values),
			Types:  sortedNames(g.types),
		})
	}
	return out
}

func sortedNames(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoaderFile is one generated client file the entrypoint loader imports, with
// the object classes it contributes to the type-name map.
type LoaderFile struct {
	// Alias is the namespace binding the loader imports the file under.
	Alias string
	// From is the specifier the loader imports the file from: the package for
	// the core library, a relative sibling for each module client (the loader
	// sits under clients/ beside them).
	From    string
	Entries []LoaderEntry
}

// LoaderEntry maps one schema type name to the class its owning file exports —
// the two differ when formatName renames a reserved word (e.g. Module ->
// Module_).
type LoaderEntry struct {
	TypeName  string
	ClassName string
}

// loaderFiles enumerates, per generated client file, the object classes the
// entrypoint loader can instantiate from an ID: every exportable object type
// with fields, keyed by its schema type name. The map is explicit rather than
// searched so two modules exporting the same class name cannot resolve by
// import order. The loader is generated only for module codegen, so core comes
// from the @dagger.io/dagger package and module clients from relative siblings.
func (funcs typescriptTemplateFuncs) loaderFiles() []LoaderFile {
	if funcs.fullSchema == nil {
		return nil
	}

	const coreSpec = "@dagger.io/dagger"
	// Keyed by owner module ("" for core) so the namespace alias derives from
	// the module name, not from the import specifier's shape.
	byOwner := map[string][]LoaderEntry{}
	for _, t := range funcs.fullSchema.Types {
		if t.Kind != introspection.TypeKindObject || len(t.Fields) == 0 || !funcs.isExportableType(t) {
			continue
		}
		owner := typeOwner(t)
		byOwner[owner] = append(byOwner[owner], LoaderEntry{
			TypeName:  t.Name,
			ClassName: funcs.exportedTypeName(t),
		})
	}

	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Slice(owners, func(i, j int) bool {
		if (owners[i] == "") != (owners[j] == "") {
			return owners[i] == ""
		}
		return owners[i] < owners[j]
	})

	out := make([]LoaderFile, 0, len(owners))
	for _, owner := range owners {
		entries := byOwner[owner]
		sort.Slice(entries, func(a, b int) bool { return entries[a].TypeName < entries[b].TypeName })
		if owner == "" {
			out = append(out, LoaderFile{Alias: "__core", From: coreSpec, Entries: entries})
			continue
		}
		// "__core" is reserved for the library, so a module named "core"
		// ("__modCore") cannot collide with it.
		out = append(out, LoaderFile{
			Alias:   "__mod" + strcase.ToCamel(funcs.depFileName(owner)),
			From:    funcs.siblingImportSpec(owner),
			Entries: entries,
		})
	}
	return out
}

// rootClientType returns the extendable root type (Query) among the file's
// types; its module-contributed fields become the file's own Client class.
func (funcs typescriptTemplateFuncs) rootClientType(types []*introspection.Type) *introspection.Type {
	for _, t := range types {
		if funcs.isExtendableType(t) {
			return t
		}
	}
	return nil
}

// exportedTypeName returns the TS identifier under which a type is exported,
// matching the per-kind naming used by the templates: objects go through
// QueryToClient+FormatName, interfaces/inputs through FormatName, while scalars
// and enums keep their raw schema name.
func (funcs typescriptTemplateFuncs) exportedTypeName(t *introspection.Type) string {
	switch t.Kind {
	case introspection.TypeKindObject:
		return funcs.formatName(funcs.queryToClient(t.Name))
	case introspection.TypeKindInterface, introspection.TypeKindInputObject:
		return funcs.formatName(t.Name)
	default:
		return t.Name
	}
}
