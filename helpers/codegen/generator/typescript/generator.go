package typescriptgenerator

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/iancoleman/strcase"
	"github.com/psanford/memfs"

	"codegen/generator"
	"codegen/generator/typescript/templates"
	"codegen/introspection"
)

const (
	// ClientGenFile is the core file name: it holds core Dagger types only and
	// belongs to the @dagger.io/dagger library (the sdk/ directory), not to any
	// one module. The library's index.ts re-exports it.
	ClientGenFile = "client.gen.ts"
	// LoaderGenFile is the entrypoint object loader. "loader" is a reserved
	// module name: a module kebab-cased to it would claim the same file.
	LoaderGenFile = "loader.gen.ts"
	// ModuleClientsDir is where a module scope puts the per-module client files
	// and the loader — beside the library, not inside it. The library (sdk/)
	// stays core-only so it can become an npm package; the clients that depend
	// on it live here and reach it through the @dagger.io/dagger specifier.
	ModuleClientsDir = "clients"
)

type TypeScriptGenerator struct {
	Config generator.Config
}

// GenerateModule generates a scope's client bindings — a module's own or a
// standalone client's: one core client.gen.ts plus one <module>.gen.ts per
// module. Layout follows Config.ModuleConfig (EmitLoader / FlatClients).
func (g *TypeScriptGenerator) GenerateModule(_ context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	return generate(g.Config, ClientGenFile, schema, schemaVersion)
}

func (g *TypeScriptGenerator) GenerateLibrary(_ context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	return generate(g.Config, ClientGenFile, schema, schemaVersion)
}

func generate(config generator.Config, target string, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	generator.SetSchema(schema)

	sort.SliceStable(schema.Types, func(i, j int) bool {
		return schema.Types[i].Name < schema.Types[j].Name
	})
	for _, v := range schema.Types {
		sort.SliceStable(v.Fields, func(i, j int) bool {
			in := v.Fields[i].Name
			jn := v.Fields[j].Name
			switch {
			case in == "id" && jn == "id":
				return false
			case in == "id":
				return true
			case jn == "id":
				return false
			default:
				return in < jn
			}
		})
	}

	// Split module-contributed types into their own <module>.gen.ts files.
	// The core file is rendered from a schema with the module-owned types
	// removed — for the extendable types (Query) the contributed fields are
	// dropped — and each per-module file is a self-contained client: its own
	// root Client built from those fields, its own dag, its own entrypoint
	// functions. Nothing merges back into the core Client.
	//
	// Every module in the schema is split, including the one being generated
	// for: its own API lands in <module>.gen.ts. In module codegen the core
	// file is the library and the module files live beside it under clients/;
	// in a standalone client they sit together in one package directory.
	splitModules := schema.DependencyNames()

	// A module scope keeps its client files in a clients/ subdirectory apart
	// from the core file (its sdk/ and src/ share the root), so the two never
	// collide by name — only the loader, their sibling, is reserved. Everything
	// else — a flat standalone client, the library's own bindings — puts the
	// client files in the output root beside the core file, so the core file's
	// name is reserved too. A flat client scope also packages each client into
	// its own directory beside the library's ("dagger"), so that name is
	// reserved there as well.
	nest := config.ModuleConfig != nil && !config.ModuleConfig.FlatClients
	emitLoader := config.ModuleConfig != nil && config.ModuleConfig.EmitLoader
	reserved := map[string]bool{}
	if emitLoader {
		reserved[strings.TrimSuffix(LoaderGenFile, ".gen.ts")] = true
	}
	if !nest {
		reserved[strings.TrimSuffix(filepath.Base(target), ".gen.ts")] = true
	}
	if config.ModuleConfig != nil && config.ModuleConfig.FlatClients {
		reserved["dagger"] = true
	}
	for _, depName := range splitModules {
		if name := strcase.ToKebab(depName); reserved[name] {
			return nil, fmt.Errorf("module name %q collides with the generated %s.gen.ts file", depName, name)
		}
	}

	moduleDir := filepath.Dir(target)
	if nest {
		moduleDir = filepath.Join(moduleDir, ModuleClientsDir)
	}

	coreSchema := schema
	if len(splitModules) > 0 {
		coreSchema = schema.Exclude(splitModules...)
	}

	// The template funcs always get the full schema so the module-splitting
	// helpers can enumerate modules regardless of which (possibly filtered)
	// schema a given file is rendered from.
	tmpl := templates.New(schemaVersion, schema, "", config)

	mfs := memfs.New()

	// Render the core file from the filtered core schema.
	if err := renderTemplate(mfs, tmpl, "api", target, depFileData{
		Schema:        coreSchema,
		SchemaVersion: schemaVersion,
		Types:         coreSchema.Types,
	}); err != nil {
		return nil, err
	}

	// The source each module client serves on use, keyed kebab-cased to match
	// the split names. A module with no entry (a manifest dependency the engine
	// serves) renders no serve hook.
	bound := map[string]generator.BoundModule{}
	if config.ModuleConfig != nil {
		for _, m := range config.ModuleConfig.BoundModules {
			bound[strcase.ToKebab(m.Name)] = m
		}
	}

	// Render one <module>.gen.ts client file per split module.
	for _, depName := range splitModules {
		depSchema := schema.Include(depName)
		depTarget := filepath.Join(moduleDir, strcase.ToKebab(depName)+".gen.ts")
		data := depFileData{
			Schema:        depSchema,
			SchemaVersion: schemaVersion,
			Types:         depSchema.Types,
			DepName:       depName,
		}
		if m, ok := bound[strcase.ToKebab(depName)]; ok {
			bm := m
			data.Bound = &bm
		}
		if err := renderTemplate(mfs, tmpl, "module_client", depTarget, data); err != nil {
			return nil, fmt.Errorf("render module %q: %w", depName, err)
		}
	}

	// A module scope also carries the entrypoint loader: the entrypoint loads
	// core objects by ID, and only codegen knows which generated file declares
	// which class. A standalone client has no entrypoint, so no loader.
	if emitLoader {
		loaderTarget := filepath.Join(moduleDir, LoaderGenFile)
		if err := renderTemplate(mfs, tmpl, "loader", loaderTarget, depFileData{
			Schema:        schema,
			SchemaVersion: schemaVersion,
			Types:         schema.Types,
		}); err != nil {
			return nil, fmt.Errorf("render loader: %w", err)
		}
	}

	return &generator.GeneratedState{
		Overlay: mfs,
	}, nil
}

// depFileData is the template "dot" for the core "api" template, the
// per-module "module_client" template and the "loader" template. DepName is
// only set for module files; it names the module the file is rendered for so
// the import planner can tell its own types from siblings'.
type depFileData struct {
	Schema        *introspection.Schema
	SchemaVersion string
	Types         []*introspection.Type
	DepName       string
	// Bound is the module's serve source, set only for a module client whose
	// module the generated client should serve on use. Nil for the core file,
	// the loader, and modules the engine serves.
	Bound *generator.BoundModule
}

// renderTemplate executes the named template against data and writes the
// result to target inside mfs.
func renderTemplate(mfs *memfs.FS, tmpl *template.Template, name, target string, data depFileData) error {
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return fmt.Errorf("render %q: %w", name, err)
	}
	if err := mfs.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", filepath.Dir(target), err)
	}
	if err := mfs.WriteFile(target, b.Bytes(), 0600); err != nil {
		return fmt.Errorf("failed to write client file at %s: %w", target, err)
	}
	return nil
}
