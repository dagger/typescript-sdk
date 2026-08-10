// Command codegen generates TypeScript bindings from a pre-computed
// introspection schema:
//
//	codegen module — a module's own embedded bindings (client.gen.ts plus one
//	                 <dep>.gen.ts per dependency), importing the bundled library
//	                 from ./core.js.
//	codegen client — a standalone client (dagger.gen.ts for the core types, plus
//	                 one <module>.gen.ts per module in the bound module's
//	                 closure), importing @dagger.io/dagger.
//	codegen library — the SDK library's own bindings, importing the runtime they
//	                 ship alongside.
//
// Generation is engine-free: the schema and the bound module's metadata are
// supplied as files, so no session is opened. `codegen introspect` is the one
// exception — it dumps the session schema the library bindings are generated
// from, over plain HTTP, and only runs in this repo's own generate step.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"codegen/generator"
	typescriptgenerator "codegen/generator/typescript"
	"codegen/introspection"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codegen:", err)
		os.Exit(1)
	}
}

// clientMeta is the bound module's metadata the SDK reads off client.module /
// client.moduleSource and writes to --client-meta-path. It mirrors the subset
// the client generator needs (see generator.ClientGeneratorConfig).
type clientMeta struct {
	ModuleName    string                `json:"moduleName"`
	EngineVersion string                `json:"engineVersion"`
	Module        generator.BoundModule `json:"module"`
}

// validateBoundModuleKind fails closed on a source kind the generated client
// has no serve path for, rather than emit a client that silently mis-serves. A
// client serves the one module it binds to: GIT_SOURCE serves from a canonical
// ref+pin; LOCAL_SOURCE and DIR_SOURCE (how a workspace-local module resolves in
// practice) serve by resolving the workspace-relative path against the workspace.
func validateBoundModuleKind(m generator.BoundModule) error {
	switch m.Kind {
	case generator.ModuleKindGit, generator.ModuleKindLocal, generator.ModuleKindDir:
		return nil
	default:
		return fmt.Errorf("bound module has unsupported source kind %q", m.Kind)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codegen <module|client> [flags]")
	}

	switch args[0] {
	case "module":
		return runModule(args[1:])
	case "client":
		return runClient(args[1:])
	case "library":
		return runLibrary(args[1:])
	case "introspect":
		return runIntrospect(args[1:])
	default:
		return fmt.Errorf("unknown command %q (want module, client, library or introspect)", args[0])
	}
}

// runLibrary regenerates the SDK library's own bindings. They ship inside the
// library, so they reach the runtime by relative source path rather than
// through the bundle or the package name — the third import arm. The schema is
// the plain session schema (see `codegen introspect`): core only, unscrubbed.
func runLibrary(args []string) error {
	fs := flag.NewFlagSet("library", flag.ExitOnError)
	var (
		introspectionPath = fs.String("introspection-json-path", "", "path to the introspection schema JSON")
		outputDir         = fs.String("output", ".", "output directory for the generated bindings")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	schema, schemaVersion, err := loadSchema(*introspectionPath)
	if err != nil {
		return err
	}

	cfg := generator.Config{OutputDir: *outputDir}
	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateLibrary(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate library: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated library bindings: %w", err)
	}

	return nil
}

func runModule(args []string) error {
	fs := flag.NewFlagSet("module", flag.ExitOnError)
	var (
		introspectionPath = fs.String("introspection-json-path", "", "path to the introspection schema JSON")
		moduleName        = fs.String("module-name", "", "name of the module to generate bindings for")
		outputDir         = fs.String("output", ".", "output directory for the generated bindings")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *moduleName == "" {
		return fmt.Errorf("--module-name is required")
	}

	schema, schemaVersion, err := loadSchema(*introspectionPath)
	if err != nil {
		return err
	}

	cfg := generator.Config{
		OutputDir:    *outputDir,
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: *moduleName},
	}
	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateModule(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate module: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated module bindings: %w", err)
	}

	return nil
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	var (
		introspectionPath = fs.String("introspection-json-path", "", "path to the introspection schema JSON")
		clientMetaPath    = fs.String("client-meta-path", "", "path to the client meta JSON (name, engineVersion, bound module)")
		outputDir         = fs.String("output", ".", "output directory for the generated client")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	schema, schemaVersion, err := loadSchema(*introspectionPath)
	if err != nil {
		return err
	}

	clientConfig := &generator.ClientGeneratorConfig{}
	if *clientMetaPath != "" {
		metaJSON, err := os.ReadFile(*clientMetaPath)
		if err != nil {
			return fmt.Errorf("read client meta json: %w", err)
		}
		var meta clientMeta
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return fmt.Errorf("unmarshal client meta json: %w", err)
		}
		clientConfig.ModuleName = meta.ModuleName
		clientConfig.EngineVersion = meta.EngineVersion
		clientConfig.BoundModule = meta.Module

		if err := validateBoundModuleKind(meta.Module); err != nil {
			return err
		}
	}

	cfg := generator.Config{
		OutputDir:    *outputDir,
		ClientConfig: clientConfig,
	}
	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateClient(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}

	return nil
}

// loadSchema reads the introspection JSON and prepares it for rendering: the
// templates walk from a field back to its parent type, a link the JSON does not
// carry.
func loadSchema(path string) (*introspection.Schema, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("--introspection-json-path is required")
	}

	introspectionJSON, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read introspection json: %w", err)
	}
	var resp introspection.Response
	if err := json.Unmarshal(introspectionJSON, &resp); err != nil {
		return nil, "", fmt.Errorf("unmarshal introspection json: %w", err)
	}
	if resp.Schema == nil {
		return nil, "", fmt.Errorf("introspection json has no __schema")
	}

	generator.SetSchemaParents(resp.Schema)

	return resp.Schema, resp.SchemaVersion, nil
}
