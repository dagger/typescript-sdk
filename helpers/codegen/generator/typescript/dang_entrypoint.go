package typescriptgenerator

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/psanford/memfs"

	"codegen/generator"
	"codegen/generator/typescript/templates"
)

// DefaultDangEntrypointFile is the path, relative to the module root, of the
// Dang program the engine loads under a manifest `[entrypoint]`. It sits outside
// sdk/ so the Dang stays out of the tree mounted as
// node_modules/@dagger.io/dagger.
const DefaultDangEntrypointFile = "entrypoint/main.dang"

// DefaultDispatchFile is the dispatcher the Dang entrypoint's call() execs.
const DefaultDispatchFile = templates.DefaultDispatchFile

// GenerateDangEntrypoint renders `entrypoint/main.dang` from a previously-emitted
// typedef JSON — the same input the TypeScript dispatcher is rendered from.
//
// The two are generated from one scan for a reason: types() here and the
// dispatcher's invoke() have to agree on every original name, or the engine will
// route a call to a function the dispatcher cannot find.
func (g *TypeScriptGenerator) GenerateDangEntrypoint(ctx context.Context) (*generator.GeneratedState, error) {
	cfg := g.Config.DangEntrypointConfig
	if cfg == nil {
		return nil, fmt.Errorf("generate-dang-entrypoint: missing DangEntrypointConfig")
	}
	if cfg.TypedefJSONPath == "" {
		return nil, fmt.Errorf("generate-dang-entrypoint: TypedefJSONPath is required")
	}
	if cfg.ClientVersion == "" {
		return nil, fmt.Errorf("generate-dang-entrypoint: ClientVersion is required")
	}

	data, err := os.ReadFile(cfg.TypedefJSONPath)
	if err != nil {
		return nil, fmt.Errorf("read typedef json %q: %w", cfg.TypedefJSONPath, err)
	}

	var module templates.TypedefModule
	if err := json.Unmarshal(data, &module); err != nil {
		return nil, fmt.Errorf("parse typedef json: %w", err)
	}

	// Which object carries the constructor is the one the engine takes for the
	// main one. A module with objects but none of them the main one has no
	// constructor to declare, and wouldn't surface: say so instead.
	names, err := templates.LoadModuleNames(ctx, cfg.Naming, &module)
	if err != nil {
		return nil, err
	}
	if main := names.MainObject(); len(module.Objects) > 0 && module.Objects[main] == nil {
		return nil, fmt.Errorf("module %q has no main object: the engine takes none of %s for it (ModuleSource.naming's mainObject: a class whose name the engine's naming rules for the module turn into the same type name as the module's)",
			cmp.Or(cfg.ModuleName, module.Name), strings.Join(slices.Sorted(maps.Keys(module.Objects)), ", "))
	}

	tmpl := templates.NewDangEntrypoint(&module, templates.DangEntrypointOptions{
		ModuleName:            cfg.ModuleName,
		Runtime:               cfg.Runtime,
		ModulePath:            cfg.ModulePath,
		PackageManager:        cfg.PackageManager,
		PackageManagerVersion: cfg.PackageManagerVersion,
		BaseImage:             cfg.BaseImage,
		ClientVersion:         cfg.ClientVersion,
		DispatchFile:          cfg.DispatchFile,
		TSConfigPath:          cfg.TSConfigPath,
		Include:               cfg.Include,
		Names:                 names,
	})

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "dang_entrypoint", &module); err != nil {
		return nil, fmt.Errorf("render dang entrypoint: %w", err)
	}

	outFile := cfg.OutputFile
	if outFile == "" {
		outFile = DefaultDangEntrypointFile
	}

	mfs := memfs.New()
	if dir := path.Dir(outFile); dir != "." {
		if err := mfs.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create %q in overlay: %w", dir, err)
		}
	}
	if err := mfs.WriteFile(outFile, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("write dang entrypoint to overlay: %w", err)
	}

	return &generator.GeneratedState{Overlay: mfs}, nil
}
