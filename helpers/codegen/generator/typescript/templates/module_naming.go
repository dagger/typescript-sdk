package templates

import (
	"strings"

	"github.com/iancoleman/strcase"

	"codegen/identifiers"
)

// moduleNamer predicts the schema names the engine gives the module's own
// declarations: the names the generated entrypoints select when they call
// the module through its own interfaces, the enum members they send, and
// which object the engine takes for the main one. It mirrors core.Namer in
// dagger/dagger (core/gqlformat.go), which names a module by the rules of the
// engine version it's served at (ModuleSource.NamingView): engine/naming from
// identifiers.CasingVersion on, strcase before it. It is the port of go-sdk's
// moduleNamer (dagger/go-sdk#90), for the declarations TypeScript predicts.
//
// The names come from the engine (identifiers.Names, at the module's
// effective version), never from a local copy of its rules: a name the
// entrypoint predicts differently than the engine names it breaks the module
// at run time (a fragment on a type that doesn't exist, an unknown enum
// member).
//
// With nil names, the legacy rules, it predicts by strcase, as the engine's
// legacy Namer names (strcase.ToCamel, ToLowerCamel, ToScreamingSnake, and
// legacyNamespaceObject). The entrypoints predicted otherwise before: field
// names as declared (getURL, which the engine names getUrl), interfaces as
// the scanner's module name followed by the interface's (NamingAppHTTPFetcher
// for the engine's NamingAppHttpFetcher, PondPondHttpClient for PondHttpClient),
// the main object by comparing the two pascalized. Those mispredictions broke
// the module wherever they differed from the engine's names, so they are
// fixed rather than kept; where they agreed, the output is the same.
//
// One legacy detail is not mirrored: the engine registers strcase acronyms
// (JSON, LLM, LLMContent, LLMContentBlock) that strcase applies only to a name
// that is exactly one of them, which no prediction here comes across in
// practice.
type moduleNamer struct {
	// names are formatted by the rules the engine names the module's
	// declarations by, or nil for the legacy rules.
	names *identifiers.Names
	// moduleName is the module's name as the engine knows it (naming-app,
	// k8s-tools): the name it namespaces the module's types with.
	moduleName string
	// typedefName is the module's name as the scanner spells it in the
	// typedef (NamingApp, K8STools, My2Go), which loses where the words are
	// cut: only a stand-in when moduleName is empty (a hand-run codegen).
	typedefName string
}

func (n moduleNamer) legacy() bool {
	return n.names == nil
}

// module is the name the engine namespaces the module's types with.
func (n moduleNamer) module() string {
	if n.moduleName != "" {
		return n.moduleName
	}
	return n.typedefName
}

// format returns name formatted in f. A name the engine can't parse stays as
// it is, as the engine leaves it (schema validation reports it). A name that
// isn't formatted yet returns false: it's formatted before the next render
// (identifiers.Names.Run), and the caller stands in its legacy name.
func (n moduleNamer) format(f identifiers.Format, name string) (string, bool) {
	if !identifiers.Valid(name) {
		return name, true
	}
	return n.names.Lookup(f, name)
}

// objectName is the schema name of an object, interface or enum: PascalCase,
// with acronyms in capitals (HTTPClient).
func (n moduleNamer) objectName(name string) string {
	if !n.legacy() {
		if formatted, ok := n.format(identifiers.TypeName, name); ok {
			return formatted
		}
	}
	return strcase.ToCamel(name)
}

// fieldName is the schema name of a function or argument: camelCase, with
// acronyms written like words (baseUrl, httpClient). strcase writes a run of
// capitals like a word too, so getURL is getUrl by either rules.
func (n moduleNamer) fieldName(name string) string {
	if !n.legacy() {
		if formatted, ok := n.format(identifiers.FieldName, name); ok {
			return formatted
		}
	}
	return strcase.ToLowerCamel(name)
}

// enumMemberName is the schema name of an enum member: names already in
// GraphQL's member convention (HTTP2, P_256) are kept, everything else
// becomes SCREAMING_SNAKE (Arm64 is ARM64, ARM_64 by strcase). Sending a
// different name than the engine registers is an unknown enum member.
func (n moduleNamer) enumMemberName(name string) string {
	if isConventionalGraphQLEnumMemberName(name) {
		return name
	}
	if !n.legacy() {
		if formatted, ok := n.format(identifiers.EnumMemberName, name); ok {
			return formatted
		}
	}
	return strcase.ToScreamingSnake(name)
}

// isConventionalGraphQLEnumMemberName reports whether name is already in
// GraphQL's enum member convention (HTTP2, P_256), which the engine keeps as
// it is (core's isConventionalGraphQLEnumMemberName).
func isConventionalGraphQLEnumMemberName(name string) bool {
	if name == "" || strings.HasPrefix(name, "__") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if i > 0 && ((c >= '0' && c <= '9') || c == '_') {
			continue
		}
		return false
	}
	return true
}

// namespaceObject maps the name of a type the module declares to its schema
// name: the module's name, followed by the type's name unless that already
// starts with the module's name. It mirrors Namer.NamespaceObject, for the
// module's view of itself, whose final and original names are the same.
func (n moduleNamer) namespaceObject(typeName string) string {
	moduleName := n.module()
	if n.legacy() {
		return legacyNamespaceObject(typeName, moduleName)
	}
	if !identifiers.Valid(typeName) || !identifiers.Valid(moduleName) {
		return n.objectName(moduleName + "_" + typeName)
	}
	typePascal, ok1 := n.names.Lookup(identifiers.TypeName, typeName)
	typeWords, ok2 := n.names.Lookup(identifiers.Words, typeName)
	modPascal, ok3 := n.names.Lookup(identifiers.TypeName, moduleName)
	modWords, ok4 := n.names.Lookup(identifiers.Words, moduleName)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		// Stand-in until the names are formatted (see format).
		return legacyNamespaceObject(typeName, moduleName)
	}
	// The engine compares words, not characters, so the module's name is
	// only a prefix on a word boundary: "Postman" in module "post" becomes
	// "PostPostman", while "PostMan" is already namespaced. Failing that,
	// words that spell the module's name once flattened count too, since
	// the type and the module may cut words differently around digits and
	// acronyms (K8STools in module k8s-tools, My2Go in module my2go).
	//
	// The engine works on parsed words; formatIdentifiers returns strings.
	// The KEBAB form gives the words (lowercase, as the engine compares them
	// case-insensitively), and the PASCAL form is the concatenation of each
	// word's capitalized form, which has the same length as the word, so it
	// splits along the same boundaries.
	words := strings.Split(typeWords, "-")
	rest := 0
	if mod := strings.Split(modWords, "-"); hasWordPrefix(words, mod) {
		rest = len(mod)
	} else if k := flatWordPrefix(words, flatName(moduleName)); k >= 0 {
		rest = k
	}
	offset := 0
	for _, w := range words[:rest] {
		offset += len(w)
	}
	if offset > len(typePascal) {
		// Unreachable unless formatIdentifiers changes its output.
		return modPascal + typePascal
	}
	return modPascal + typePascal[offset:]
}

// hasWordPrefix reports whether words starts with prefix.
func hasWordPrefix(words, prefix []string) bool {
	if len(prefix) > len(words) {
		return false
	}
	for i, w := range prefix {
		if words[i] != w {
			return false
		}
	}
	return true
}

// flatWordPrefix returns how many of words, flattened and joined, spell
// prefix exactly, ending on a word boundary; or -1.
func flatWordPrefix(words []string, prefix string) int {
	if prefix == "" {
		return -1
	}
	var spelled string
	for i, w := range words {
		spelled += flatName(w)
		switch {
		case spelled == prefix:
			return i + 1
		case len(spelled) >= len(prefix) || !strings.HasPrefix(prefix, spelled):
			return -1
		}
	}
	return -1
}

// flatName is name lowercased, with everything but ASCII letters and digits
// dropped: the spelling every casing of a name shares, however a naming
// rule cuts it into words (K8STools, K8sTools and k8s-tools are k8stools).
func flatName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9':
			b.WriteByte(c)
		case 'A' <= c && c <= 'Z':
			b.WriteByte(c + 'a' - 'A')
		}
	}
	return b.String()
}

// legacyNamespaceObject is namespaceObject by the strcase rules: the engine's
// legacyNamespaceObject (core/gqlformat.go), unchanged from before
// engine/naming, for a module whose final and original names are the same.
func legacyNamespaceObject(typeName, moduleName string) string {
	typeName = strcase.ToCamel(typeName)
	modName := strcase.ToCamel(moduleName)
	if rest := strings.TrimPrefix(typeName, modName); rest != typeName {
		if len(rest) == 0 {
			// The main object keeps the module's name.
			return modName
		}
		// Only treat the prefix as a namespace on a word boundary: type
		// "Postman" in module "post" must become "PostPostman", while
		// "PostMan" is already namespaced.
		if 'A' <= rest[0] && rest[0] <= 'Z' {
			return strcase.ToCamel(moduleName + rest)
		}
	}
	return strcase.ToCamel(moduleName + "_" + typeName)
}

// sameDeclaredName mirrors Namer.SameDeclaredName: whether a and b, two
// spellings of a declared name, name the same thing, such as a module's name
// and its main object. They normalize to the same schema name, or, with the
// engine/naming rules, they're the same once flattened (K8STools for
// k8s-tools, My2Go for my2go).
func (n moduleNamer) sameDeclaredName(a, b string) bool {
	if n.objectName(a) == n.objectName(b) {
		return true
	}
	return !n.legacy() && flatName(a) != "" && flatName(a) == flatName(b)
}

// isMainObject reports whether the object named name is the module's main
// object, as the engine finds it for a module whose runtime registers its
// objects by name (Module.objectTypeDefResultByOriginalName): the object whose
// name normalizes like the module's, or else, by the engine's naming rules,
// the only one that's the same once flattened. objects are the names of
// every object the module declares.
func (n moduleNamer) isMainObject(name string, objects []string) bool {
	moduleName := n.module()
	if n.objectName(name) == n.objectName(moduleName) {
		return true
	}
	if !n.sameDeclaredName(name, moduleName) {
		return false
	}
	for _, other := range objects {
		if other != name && n.sameDeclaredName(other, moduleName) {
			// Another exact match wins, and several flat ones match nothing.
			return false
		}
	}
	return true
}

// PrefetchModuleNames records every name a module's entrypoints predict in
// names (nil for the legacy rules, which predict nothing), so that the first
// render finds them formatted and generation takes one round trip.
// moduleName is the module's name as the engine knows it; empty uses the
// typedef's.
func PrefetchModuleNames(module *TypedefModule, moduleName string, names *identifiers.Names) {
	moduleNamer{names: names, moduleName: moduleName, typedefName: module.Name}.prefetch(module)
}

// prefetch records every name the module's entrypoints predict.
func (n moduleNamer) prefetch(module *TypedefModule) {
	if n.legacy() {
		return
	}
	moduleName := n.module()
	n.names.Prefetch(identifiers.TypeName, moduleName)
	n.names.Prefetch(identifiers.Words, moduleName)
	for _, obj := range module.Objects {
		n.names.Prefetch(identifiers.TypeName, obj.Name)
	}
	for _, iface := range module.Interfaces {
		n.names.Prefetch(identifiers.TypeName, iface.Name)
		n.names.Prefetch(identifiers.Words, iface.Name)
		for _, fn := range iface.Functions {
			n.names.Prefetch(identifiers.FieldName, fn.Name)
			for _, arg := range fn.Arguments {
				n.names.Prefetch(identifiers.FieldName, arg.Name)
			}
		}
	}
	for _, enum := range module.Enums {
		for _, v := range enum.Values {
			if !isConventionalGraphQLEnumMemberName(v.Name) {
				n.names.Prefetch(identifiers.EnumMemberName, v.Name)
			}
		}
	}
}
