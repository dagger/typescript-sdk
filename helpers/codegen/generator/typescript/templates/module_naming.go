package templates

import (
	"strings"

	"github.com/iancoleman/strcase"
)

// moduleNamer predicts the schema names the engine gives the module's own
// declarations: the names the generated entrypoints select when they call
// the module through its own interfaces, and which object the engine takes
// for the main one. It mirrors the engine's legacy naming rules (core.Namer
// in dagger/dagger, core/gqlformat.go, by strcase: ToCamel, ToLowerCamel, and
// legacyNamespaceObject), which every released engine names modules by.
//
// The entrypoints used to predict otherwise: field names as declared (getURL,
// which the engine names getUrl), interfaces as the scanner's module name
// followed by the interface's (NamingAppHTTPFetcher for the engine's
// NamingAppHttpfetcher, PondPondHttpClient for PondHttpClient), and the main
// object by comparing the two pascalized. Those mispredictions broke the
// module wherever they differed from the engine's names; where they agreed,
// the output is the same.
//
// One legacy detail is not mirrored: the engine registers strcase acronyms
// (JSON, LLM, LLMContent, LLMContentBlock) that strcase applies only to a name
// that is exactly one of them, which no prediction here comes across in
// practice.
type moduleNamer struct {
	// moduleName is the module's name as the engine knows it (naming-app,
	// k8s-tools): the name it namespaces the module's types with.
	moduleName string
	// typedefName is the module's name as the scanner spells it in the
	// typedef (NamingApp, K8STools, My2Go), which loses where the words are
	// cut: only a stand-in when moduleName is empty (a hand-run codegen).
	typedefName string
}

// module is the name the engine namespaces the module's types with.
func (n moduleNamer) module() string {
	if n.moduleName != "" {
		return n.moduleName
	}
	return n.typedefName
}

// objectName is the schema name of an object, interface or enum.
func (n moduleNamer) objectName(name string) string {
	return strcase.ToCamel(name)
}

// fieldName is the schema name of a function or argument. strcase writes a
// run of capitals like a word, so getURL is getUrl.
func (n moduleNamer) fieldName(name string) string {
	return strcase.ToLowerCamel(name)
}

// namespaceObject maps the name of a type the module declares to its schema
// name: the module's name, followed by the type's name unless that already
// starts with the module's name. It mirrors Namer.NamespaceObject, for the
// module's view of itself, whose final and original names are the same.
func (n moduleNamer) namespaceObject(typeName string) string {
	return legacyNamespaceObject(typeName, n.module())
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

// isMainObject reports whether the object named name is the module's main
// object, as the engine finds it for a module whose runtime registers its
// objects by name (Module.objectTypeDefResultByOriginalName): the object whose
// name normalizes like the module's.
func (n moduleNamer) isMainObject(name string) bool {
	return n.objectName(name) == n.objectName(n.module())
}
