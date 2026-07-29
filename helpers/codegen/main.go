// Command codegen generates a standalone TypeScript client (dagger.gen.ts for
// the core types, plus one <module>.gen.ts per module in the bound module's
// closure) from a pre-computed introspection schema.
//
// It is intentionally engine-free: the schema and the bound module's metadata
// are supplied as files, so no nested engine session is opened.
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
	if err := run(); err != nil {
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

func run() error {
	var (
		introspectionPath = flag.String("introspection-json-path", "", "path to the introspection schema JSON")
		clientMetaPath    = flag.String("client-meta-path", "", "path to the client meta JSON (name, engineVersion, dependencies)")
		outputDir         = flag.String("output", ".", "output directory for the generated client")
	)
	flag.Parse()

	if *introspectionPath == "" {
		return fmt.Errorf("--introspection-json-path is required")
	}

	introspectionJSON, err := os.ReadFile(*introspectionPath)
	if err != nil {
		return fmt.Errorf("read introspection json: %w", err)
	}
	var resp introspection.Response
	if err := json.Unmarshal(introspectionJSON, &resp); err != nil {
		return fmt.Errorf("unmarshal introspection json: %w", err)
	}
	if resp.Schema == nil {
		return fmt.Errorf("introspection json has no __schema")
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
		Lang:         generator.SDKLangTypeScript,
		OutputDir:    *outputDir,
		ClientConfig: clientConfig,
	}

	generator.SetSchemaParents(resp.Schema)

	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateClient(ctx, resp.Schema, resp.SchemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}

	return nil
}
