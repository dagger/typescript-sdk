package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// DEMO: ModuleSource.naming does not exist in any engine yet. This file
// pretends it does (see typescript-sdk.dang, moduleNamesJSON), to show how
// much of the naming machinery it would delete.

// ModuleNames are the schema names the engine gives the module's own
// declarations, as ModuleSource.naming answered the SDK: the names the
// generated entrypoints select when they call the module through its own
// interfaces, the enum members they send, and which object the engine takes
// for the main one. The engine answers by the rules of the version it serves
// the module at, strcase for a legacy module, so nothing here knows a rule or
// a version: a name is looked up.
type ModuleNames struct {
	// TypeNames are the namespaced schema names of the module's interfaces.
	TypeNames map[string]string
	// FieldNames are the schema names of their functions and arguments.
	FieldNames map[string]string
	// EnumMemberNames are the schema names of the module's enum members.
	EnumMemberNames map[string]string
	// MainObject is the object the engine takes for the main one, or "" for
	// none.
	MainObject string

	// unanswered are the names looked up that the engine wasn't asked about.
	unanswered []string
}

// moduleNamesJSON is how the SDK hands ModuleNames over: for each table, the
// names it asked about and the engine's answers, in the same order.
type moduleNamesJSON struct {
	TypeNames       namePairs `json:"typeNames"`
	FieldNames      namePairs `json:"fieldNames"`
	EnumMemberNames namePairs `json:"enumMemberNames"`
	MainObject      string    `json:"mainObject"` // null: none
}

type namePairs struct {
	Names     []string `json:"names"`
	Formatted []string `json:"formatted"`
}

func (p namePairs) table(what string) (map[string]string, error) {
	if len(p.Names) != len(p.Formatted) {
		return nil, fmt.Errorf("%s: %d answers for %d names", what, len(p.Formatted), len(p.Names))
	}
	table := make(map[string]string, len(p.Names))
	for i, name := range p.Names {
		table[name] = p.Formatted[i]
	}
	return table, nil
}

// LoadModuleNames reads the names the SDK got from ModuleSource.naming, from
// path. Without a path no name is answered, which only a module that
// declares no interfaces or enums renders with.
func LoadModuleNames(path string) (*ModuleNames, error) {
	if path == "" {
		return &ModuleNames{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module names: %w", err)
	}
	var raw moduleNamesJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse module names: %w", err)
	}
	names := &ModuleNames{MainObject: raw.MainObject}
	if names.TypeNames, err = raw.TypeNames.table("typeNames"); err != nil {
		return nil, err
	}
	if names.FieldNames, err = raw.FieldNames.table("fieldNames"); err != nil {
		return nil, err
	}
	if names.EnumMemberNames, err = raw.EnumMemberNames.table("enumMemberNames"); err != nil {
		return nil, err
	}
	return names, nil
}

// Unanswered fails when a render looked up a name the engine wasn't asked
// about: the entrypoint would select a name the engine may not use, and break
// the module at run time.
func (n *ModuleNames) Unanswered() error {
	if len(n.unanswered) == 0 {
		return nil
	}
	slices.Sort(n.unanswered)
	return fmt.Errorf("module names: the engine wasn't asked about %s", strings.Join(slices.Compact(n.unanswered), ", "))
}

func (n *ModuleNames) lookup(table map[string]string, name string) string {
	if formatted, ok := table[name]; ok {
		return formatted
	}
	n.unanswered = append(n.unanswered, name)
	return name
}

// typeName is the namespaced schema name of one of the module's interfaces,
// which calls through it select (Namer.NamespaceObject).
func (n *ModuleNames) typeName(name string) string {
	return n.lookup(n.TypeNames, name)
}

// fieldName is the schema name of a function or argument of one of the
// module's interfaces.
func (n *ModuleNames) fieldName(name string) string {
	return n.lookup(n.FieldNames, name)
}

// enumMemberName is the schema name of an enum member. Sending a different
// name than the engine registers is an unknown enum member.
func (n *ModuleNames) enumMemberName(name string) string {
	return n.lookup(n.EnumMemberNames, name)
}
