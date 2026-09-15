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
	// ClientGenFile is the core file name for module codegen: it holds core
	// Dagger types only and belongs to the @dagger.io/dagger library (the sdk/
	// directory), not to any one module. The library's index.ts re-exports it.
	ClientGenFile = "client.gen.ts"
	// CoreGenFile is the core file name for a standalone client: it holds only
	// core Dagger types, with every module (including the bound one) split into
	// its own <module>.gen.ts. Named to match the Go SDK's dagger.gen.go.
	CoreGenFile = "dagger.gen.ts"
	// LoaderGenFile is the entrypoint object loader. "loader" is a reserved
	// module name: a module kebab-cased to it would claim the same file.
	LoaderGenFile = "loader.gen.ts"
	// ModuleClientsDir is where module codegen puts the per-module client files
	// and the loader — beside the library, not inside it. The library (sdk/)
	// stays core-only so it can become an npm package; the clients that depend
	// on it live here and reach it through the @dagger.io/dagger specifier.
	ModuleClientsDir = "clients"
)

type TypeScriptGenerator struct {
	Config generator.Config
}

// GenerateModule generates a module's own embedded bindings, flat in the output
// directory: the caller lays the result down as the module's sdk/ directory.
func (g *TypeScriptGenerator) GenerateModule(_ context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	return generate(g.Config, ClientGenFile, schema, schemaVersion)
}

func (g *TypeScriptGenerator) GenerateClient(_ context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	return generate(g.Config, CoreGenFile, schema, schemaVersion)
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

	// In module codegen the module files live under clients/, a directory apart
	// from the library's core file, so the two can never collide by name — only
	// the loader, a sibling of the module files, is reserved. A standalone
	// client keeps them together, so its core file's name is reserved too.
	module := config.ModuleConfig != nil
	reserved := map[string]bool{strings.TrimSuffix(LoaderGenFile, ".gen.ts"): true}
	if !module {
		reserved[strings.TrimSuffix(filepath.Base(target), ".gen.ts")] = true
	}
	for _, depName := range splitModules {
		if name := strcase.ToKebab(depName); reserved[name] {
			return nil, fmt.Errorf("module name %q collides with the generated %s.gen.ts file", depName, name)
		}
	}

	// Module files (and the loader) land under clients/ in module codegen; a
	// standalone client keeps everything in the output root.
	moduleDir := filepath.Dir(target)
	if module {
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

	// Render one <module>.gen.ts client file per split module.
	for _, depName := range splitModules {
		depSchema := schema.Include(depName)
		depTarget := filepath.Join(moduleDir, strcase.ToKebab(depName)+".gen.ts")
		if err := renderTemplate(mfs, tmpl, "module_client", depTarget, depFileData{
			Schema:        depSchema,
			SchemaVersion: schemaVersion,
			Types:         depSchema.Types,
			DepName:       depName,
		}); err != nil {
			return nil, fmt.Errorf("render module %q: %w", depName, err)
		}
	}

	// Module bindings also carry the entrypoint loader: the generated
	// entrypoints receive core- and module-typed values as IDs, and with every
	// module in its own file only codegen knows which file declares which
	// class. A standalone client has no entrypoint, so no loader.
	if module {
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

// selfModuleName returns the name of the module the client is generated for
// (from the module or client config), or "" when generating outside a module
// (e.g. the SDK's own library client).
func selfModuleName(config generator.Config) string {
	if config.ModuleConfig != nil {
		return config.ModuleConfig.ModuleName
	}
	if config.ClientConfig != nil {
		return config.ClientConfig.ModuleName
	}
	return ""
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
