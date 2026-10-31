package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"codegen/generator"
)

// ModuleNames are the schema names the engine gives the module's own
// declarations: the names the generated entrypoints select when they call the
// module through its own interfaces, the enum members they send and receive,
// and which object the engine takes for the main one. They come from the
// engine (ModuleSource.naming), which names a module by the rules of the
// version it names it by: the engine's identifier naming rules from
// v1.0.0-beta.22 on, strcase before it. Nothing here predicts a name: one the
// entrypoint spelled differently than the engine would break the module at run
// time (a fragment on a type that doesn't exist, a field that isn't there, an
// unknown enum member).
//
// Every name the module declares that an entrypoint selects is asked for up
// front, in one query (LoadModuleNames). A name looked up that wasn't is asked
// for directly. A failed answer fails generation.
type ModuleNames struct {
	ctx    context.Context
	naming generator.ModuleNaming

	typeNames       map[string]string
	fieldNames      map[string]string
	enumMemberNames map[string]string
	// mainObject is the object the engine takes for the main one, or "".
	mainObject string
}

// namingQuery selects fields of the naming of the module source with ID $id;
// the first verb declares the fields' variables, the second selects them.
const namingQuery = `query ModuleNaming($id: ID!%s) {
  node(id: $id) {
    ... on ModuleSource {
      naming {
        %s
      }
    }
  }
}`

// ask selects fields of ModuleSource.naming, with their variables declared by
// params, and decodes the selected ModuleNaming into data.
func (n *ModuleNames) ask(params, fields string, variables map[string]any, data any) error {
	if n.naming.Engine == nil || n.naming.SourceID == "" {
		return fmt.Errorf("module names: no module source to ask the engine about")
	}
	vars := map[string]any{"id": n.naming.SourceID}
	maps.Copy(vars, variables)
	var resp struct {
		Node *struct {
			Naming json.RawMessage `json:"naming"`
		} `json:"node"`
	}
	if err := n.naming.Engine.Query(n.ctx, fmt.Sprintf(namingQuery, params, fields), vars, &resp); err != nil {
		return fmt.Errorf("module names: %w", err)
	}
	if resp.Node == nil || resp.Node.Naming == nil {
		return fmt.Errorf("module names: %q is not a module source", n.naming.SourceID)
	}
	return json.Unmarshal(resp.Node.Naming, data)
}

// LoadModuleNames asks the engine, in one query, for the names of every
// declaration of module the entrypoints select: its interfaces, their
// functions and arguments, the members of its enums, and which of its objects
// is the main one. Without a module source to ask about, nothing is asked: a
// module that selects none of its own names renders the same, and one that
// does fails to.
func LoadModuleNames(ctx context.Context, naming generator.ModuleNaming, module *TypedefModule) (*ModuleNames, error) {
	n := &ModuleNames{
		ctx:             ctx,
		naming:          naming,
		typeNames:       map[string]string{},
		fieldNames:      map[string]string{},
		enumMemberNames: map[string]string{},
	}
	if naming.Engine == nil {
		return n, nil
	}
	types, fields, members := []string{}, []string{}, []string{}
	for _, iface := range module.Interfaces {
		types = append(types, iface.Name)
		for _, fn := range iface.Functions {
			fields = append(fields, fn.Name)
			for _, arg := range fn.Arguments {
				fields = append(fields, arg.Name)
			}
		}
	}
	for _, enum := range module.Enums {
		for _, v := range enum.Values {
			members = append(members, v.Name)
		}
	}
	types, fields, members = sortedUnique(types), sortedUnique(fields), sortedUnique(members)
	// The engine takes the first candidate that matches, so they go in the
	// order the entrypoint registers the objects in.
	objects := sortedObjectKeys(module.Objects)
	var answers struct {
		TypeNames       []string `json:"typeNames"`
		FieldNames      []string `json:"fieldNames"`
		EnumMemberNames []string `json:"enumMemberNames"`
		MainObject      *string  `json:"mainObject"`
	}
	if err := n.ask(
		", $types: [String!]!, $fields: [String!]!, $members: [String!]!, $objects: [String!]!",
		"typeNames(names: $types)\n        fieldNames(names: $fields)\n        enumMemberNames(names: $members)\n        mainObject(candidates: $objects)",
		map[string]any{"types": types, "fields": fields, "members": members, "objects": objects},
		&answers,
	); err != nil {
		return nil, err
	}
	if err := zip(n.typeNames, types, answers.TypeNames); err != nil {
		return nil, err
	}
	if err := zip(n.fieldNames, fields, answers.FieldNames); err != nil {
		return nil, err
	}
	if err := zip(n.enumMemberNames, members, answers.EnumMemberNames); err != nil {
		return nil, err
	}
	if answers.MainObject != nil {
		n.mainObject = *answers.MainObject
	}
	return n, nil
}

// lookup returns the engine's name for name from table, asking field of the
// naming directly when it isn't there yet.
func (n *ModuleNames) lookup(field string, table map[string]string, name string) (string, error) {
	if formatted, ok := table[name]; ok {
		return formatted, nil
	}
	answers := map[string][]string{}
	if err := n.ask(", $names: [String!]!", field+"(names: $names)", map[string]any{"names": []string{name}}, &answers); err != nil {
		return "", err
	}
	if err := zip(table, []string{name}, answers[field]); err != nil {
		return "", err
	}
	return table[name], nil
}

// typeName is the schema name of one of the module's interfaces, namespaced
// under the module (ModuleNaming.typeNames).
func (n *ModuleNames) typeName(name string) (string, error) {
	return n.lookup("typeNames", n.typeNames, name)
}

// fieldName is the schema name of a function or argument of one of the
// module's interfaces (ModuleNaming.fieldNames).
func (n *ModuleNames) fieldName(name string) (string, error) {
	return n.lookup("fieldNames", n.fieldNames, name)
}

// enumMemberName is the schema name of a member of one of the module's enums
// (ModuleNaming.enumMemberNames).
func (n *ModuleNames) enumMemberName(name string) (string, error) {
	return n.lookup("enumMemberNames", n.enumMemberNames, name)
}

// MainObject is the object the engine takes for the module's main one
// (ModuleNaming.mainObject), or "" for none.
func (n *ModuleNames) MainObject() string {
	return n.mainObject
}

// zip records each of names with the engine's answer for it, in input order.
func zip(table map[string]string, names, answers []string) error {
	if len(answers) != len(names) {
		return fmt.Errorf("module names: %d answers for %d names", len(answers), len(names))
	}
	for i, name := range names {
		table[name] = answers[i]
	}
	return nil
}

func sortedUnique(names []string) []string {
	slices.Sort(names)
	return slices.Compact(names)
}
