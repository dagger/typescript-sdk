package typescriptgenerator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/psanford/memfs"

	"codegen/generator"
	"codegen/generator/typescript/templates"
	"codegen/introspection"
)

// DefaultEntrypointFile is the filename of the static dispatch entrypoint
// the runtime expects at the user's module root.
const DefaultEntrypointFile = "__dagger.entrypoint.ts"

// GenerateEntrypoint renders the static dispatch `__dagger.entrypoint.ts`
// for the user's module from a previously-emitted typedef JSON. The path to
// that JSON, the module root, and other options come from
// `Config.EntrypointConfig`.
func (g *TypeScriptGenerator) GenerateEntrypoint(ctx context.Context) (*generator.GeneratedState, error) {
	cfg := g.Config.EntrypointConfig
	if cfg == nil {
		return nil, fmt.Errorf("generate-entrypoint: missing EntrypointConfig")
	}
	if cfg.TypedefJSONPath == "" {
		return nil, fmt.Errorf("generate-entrypoint: TypedefJSONPath is required")
	}

	data, err := os.ReadFile(cfg.TypedefJSONPath)
	if err != nil {
		return nil, fmt.Errorf("read typedef json %q: %w", cfg.TypedefJSONPath, err)
	}

	var module templates.TypedefModule
	if err := json.Unmarshal(data, &module); err != nil {
		return nil, fmt.Errorf("parse typedef json: %w", err)
	}

	outFile := cfg.OutputFile
	if outFile == "" {
		outFile = DefaultEntrypointFile
		if cfg.DispatchMode {
			outFile = DefaultDispatchFile
		}
	}

	opts := templates.EntrypointOptions{
		SDKImportPath:    cfg.SDKImportPath,
		ModuleRoot:       cfg.ModuleRoot,
		SourceDir:        cfg.SourceDir,
		DispatchMode:     cfg.DispatchMode,
		LoaderImportPath: cfg.LoaderImportPath,
	}
	if cfg.IntrospectionJSONPath != "" {
		schemaJSON, err := os.ReadFile(cfg.IntrospectionJSONPath)
		if err != nil {
			return nil, fmt.Errorf("read introspection json %q: %w", cfg.IntrospectionJSONPath, err)
		}
		var resp introspection.Response
		if err := json.Unmarshal(schemaJSON, &resp); err != nil {
			return nil, fmt.Errorf("parse introspection json: %w", err)
		}
		// The same gate as the bindings: the module's schema is served at the
		// module's engine version, and has Query.formatIdentifiers from
		// v1.0.0-beta.21 — but beta.19 and beta.20 engines serve the field
		// too while still naming modules with strcase, so the engine's own
		// version has to be past the cut as well.
		opts.EngineNaming = resp.Schema.UsesEngineNaming(g.Config.EngineVersion)
		opts.NamingVersion = resp.SchemaVersion
	}
	tmpl := templates.NewEntrypoint(&module, opts)

	topLevel := "entrypoint"
	if cfg.DispatchMode {
		topLevel = "dispatch_entrypoint"
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, topLevel, &module); err != nil {
		return nil, fmt.Errorf("render entrypoint: %w", err)
	}

	mfs := memfs.New()
	if err := mfs.WriteFile(outFile, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("write entrypoint to overlay: %w", err)
	}

	return &generator.GeneratedState{Overlay: mfs}, nil
}
