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
//	codegen entrypoint — a module's static dispatch entrypoint, from the typedef
//	                 JSON the SDK introspector emits.
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

// clientMeta is the scope's client metadata, written by the SDK to
// --client-meta-path. One generated package serves every module in a scope, so
// this carries a list: each entry names a module, points at its client-facing
// schema, and records the provenance the serve bootstrap needs.
type clientMeta struct {
	EngineVersion string             `json:"engineVersion"`
	Modules       []clientMetaModule `json:"modules"`
}

// clientMetaModule is one bound module plus the path its schema was staged at.
// Self marks the module the bindings are being generated for, which is folded in
// differently (see foldClientsIntoModuleSchema).
type clientMetaModule struct {
	generator.BoundModule
	SchemaPath string `json:"schemaPath"`
	Self       bool   `json:"self,omitempty"`
}

// validateBoundModuleKind fails closed on a source kind the generated client
// has no serve path for, rather than emit a client that silently mis-serves:
// GIT_SOURCE serves from a canonical ref+pin; LOCAL_SOURCE and DIR_SOURCE (how a
// workspace-local module resolves in practice) serve by resolving the
// workspace-relative path against the workspace.
func validateBoundModuleKind(m generator.BoundModule) error {
	switch m.Kind {
	case generator.ModuleKindGit, generator.ModuleKindLocal, generator.ModuleKindDir:
		return nil
	default:
		return fmt.Errorf("bound module %q has unsupported source kind %q", m.Name, m.Kind)
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
	case "entrypoint":
		return runEntrypoint(args[1:])
	case "introspect":
		return runIntrospect(args[1:])
	default:
		return fmt.Errorf("unknown command %q (want module, client, library, entrypoint or introspect)", args[0])
	}
}

// runEntrypoint renders a module's static dispatch entrypoint. Unlike the
// binding generators it never sees the schema: it works from the typedef JSON
// the SDK introspector emits by scanning the user's own source, which is what
// carries the per-declaration source locations the dispatcher imports classes
// from.
func runEntrypoint(args []string) error {
	fs := flag.NewFlagSet("entrypoint", flag.ExitOnError)
	var (
		typedefPath = fs.String("typedef-json-path", "", "path to the typedef JSON emitted by the SDK introspector")
		outputDir   = fs.String("output", ".", "output directory for the generated entrypoint")
		outputFile  = fs.String("output-file", typescriptgenerator.DefaultEntrypointFile, "filename to write within the output directory")
		moduleRoot  = fs.String("module-root", "", "absolute path of the module root, used to resolve source-import paths")
		sdkImport   = fs.String("sdk-import", "@dagger.io/dagger", "bare specifier the entrypoint imports runtime helpers from")
		sourceDir   = fs.String("source-dir", "src", "the module's source directory, relative to its root")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *typedefPath == "" {
		return fmt.Errorf("--typedef-json-path is required")
	}

	cfg := generator.Config{
		OutputDir: *outputDir,
		EntrypointConfig: &generator.EntrypointGeneratorConfig{
			TypedefJSONPath: *typedefPath,
			OutputFile:      *outputFile,
			ModuleRoot:      *moduleRoot,
			SDKImportPath:   *sdkImport,
			SourceDir:       *sourceDir,
		},
	}
	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := gen.GenerateEntrypoint(ctx)
	if err != nil {
		return fmt.Errorf("generate entrypoint: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated entrypoint: %w", err)
	}

	return nil
}

// renderFunc is a generator method that turns a schema into files. The three
// schema-driven modes differ only in which one they pick, so they are passed as
// method expressions (see generateFromSchema).
type renderFunc func(*typescriptgenerator.TypeScriptGenerator, context.Context, *introspection.Schema, string) (*generator.GeneratedState, error)

// generateFromSchema is the half the schema-driven modes share: read the schema,
// build the generator, render, write the result. What differs is the config each
// mode contributes and the method it renders with, so those come in as
// arguments. `kind` names the output in errors ("module bindings", "client").
func generateFromSchema(kind, introspectionPath string, cfg generator.Config, render renderFunc) error {
	schema, schemaVersion, err := loadSchema(introspectionPath)
	if err != nil {
		return err
	}

	gen := &typescriptgenerator.TypeScriptGenerator{Config: cfg}

	ctx := context.Background()
	state, err := render(gen, ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate %s: %w", kind, err)
	}

	if err := generator.Overlay(ctx, state.Overlay, cfg.OutputDir); err != nil {
		return fmt.Errorf("write generated %s: %w", kind, err)
	}

	return nil
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

	return generateFromSchema(
		"library bindings",
		*introspectionPath,
		generator.Config{OutputDir: *outputDir},
		(*typescriptgenerator.TypeScriptGenerator).GenerateLibrary,
	)
}

func runModule(args []string) error {
	fs := flag.NewFlagSet("module", flag.ExitOnError)
	var (
		introspectionPath = fs.String("introspection-json-path", "", "path to the introspection schema JSON")
		moduleName        = fs.String("module-name", "", "name of the module to generate bindings for")
		clientMetaPath    = fs.String("client-meta-path", "", "path to the client meta JSON whose modules are folded into these bindings")
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

	if *clientMetaPath != "" {
		meta, err := loadClientMeta(*clientMetaPath)
		if err != nil {
			return err
		}
		schema, err = foldClientsIntoModuleSchema(schema, meta.Modules)
		if err != nil {
			return err
		}
		generator.SetSchemaParents(schema)
	}

	gen := &typescriptgenerator.TypeScriptGenerator{Config: generator.Config{
		OutputDir:    *outputDir,
		ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: *moduleName},
	}}

	ctx := context.Background()
	state, err := gen.GenerateModule(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate module bindings: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, *outputDir); err != nil {
		return fmt.Errorf("write generated module bindings: %w", err)
	}

	return nil
}

// loadClientMeta reads the scope's client metadata. Both generators consume it:
// the client one to render the package, the module one to fold the same targets
// into the module's own bindings.
func loadClientMeta(path string) (clientMeta, error) {
	var meta clientMeta
	metaJSON, err := os.ReadFile(path)
	if err != nil {
		return meta, fmt.Errorf("read client meta json: %w", err)
	}
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return meta, fmt.Errorf("unmarshal client meta json: %w", err)
	}
	return meta, nil
}

// foldClientsIntoModuleSchema adds each client target's own types to the
// module-facing schema, so a scope that generates a client for a module also
// gives the module source that binds it — `sdk/<target>.gen.ts` beside
// `client.gen.ts`, re-exported through @dagger.io/dagger.
//
// Only what the target contributes is folded in. A client's schema is core plus
// one module, and its core half is the *client-facing* one, which hides nothing;
// merging that wholesale would pull core types into a module's bindings that the
// module-facing schema deliberately withholds. Include keeps the target's own
// types and, on the extendable types, only the fields it contributed.
func foldClientsIntoModuleSchema(
	schema *introspection.Schema,
	modules []clientMetaModule,
) (*introspection.Schema, error) {
	if len(modules) == 0 {
		return schema, nil
	}

	schemas := []*introspection.Schema{schema}
	for _, module := range modules {
		clientSchema, _, err := loadSchema(module.SchemaPath)
		if err != nil {
			return nil, fmt.Errorf("client %q: %w", module.Name, err)
		}
		if module.Self {
			schemas = append(schemas, selfContribution(clientSchema, module.Name))
			continue
		}
		// Ask the schema which modules it carries rather than trusting the
		// recorded name: the split is driven by source-map directives, and a
		// target's directive name is the only one the filter matches.
		owned := clientSchema.DependencyNames()
		if len(owned) == 0 {
			continue
		}
		schemas = append(schemas, clientSchema.Include(owned...))
	}

	return mergeSchemas(schemas), nil
}

func runClient(args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	var (
		clientMetaPath = fs.String("client-meta-path", "", "path to the client meta JSON (engineVersion, bound modules)")
		outputDir      = fs.String("output", ".", "output directory for the generated client")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *clientMetaPath == "" {
		return fmt.Errorf("--client-meta-path is required")
	}

	meta, err := loadClientMeta(*clientMetaPath)
	if err != nil {
		return err
	}
	if len(meta.Modules) == 0 {
		return fmt.Errorf("client meta json lists no modules")
	}

	clientConfig := &generator.ClientGeneratorConfig{EngineVersion: meta.EngineVersion}
	schemas := make([]*introspection.Schema, 0, len(meta.Modules))
	schemaVersion := ""
	for _, module := range meta.Modules {
		if err := validateBoundModuleKind(module.BoundModule); err != nil {
			return err
		}
		schema, version, err := loadSchema(module.SchemaPath)
		if err != nil {
			return fmt.Errorf("module %q: %w", module.Name, err)
		}
		schemas = append(schemas, schema)
		schemaVersion = version
		clientConfig.BoundModules = append(clientConfig.BoundModules, module.BoundModule)
	}

	schema := mergeSchemas(schemas)
	generator.SetSchemaParents(schema)

	gen := &typescriptgenerator.TypeScriptGenerator{Config: generator.Config{
		OutputDir:    *outputDir,
		ClientConfig: clientConfig,
	}}

	ctx := context.Background()
	state, err := gen.GenerateClient(ctx, schema, schemaVersion)
	if err != nil {
		return fmt.Errorf("generate client: %w", err)
	}

	if err := generator.Overlay(ctx, state.Overlay, *outputDir); err != nil {
		return fmt.Errorf("write generated client: %w", err)
	}

	return nil
}

// mergeSchemas folds every target's client-facing schema into one.
//
// Each target's schema is core plus that one module, so the core API repeats
// across all of them while each contributes its own types and its own fields on
// the extendable types (Query/Binding/Env). Union by name is therefore enough:
// the first schema supplies core, and every later one adds only what is new.
// Rendering that union once is what lets a scope's package hold a single copy of
// the core API however many modules it serves.
func mergeSchemas(schemas []*introspection.Schema) *introspection.Schema {
	merged := schemas[0]
	for _, schema := range schemas[1:] {
		for _, typ := range schema.Types {
			existing := merged.Types.Get(typ.Name)
			if existing == nil {
				merged.Types = append(merged.Types, typ)
				continue
			}
			mergeTypeMembers(existing, typ)
		}
	}
	return merged
}

// mergeTypeMembers adds the members of `from` that `into` does not already
// declare. Only the extendable types actually differ between schemas, but this
// stays general so a core type gaining a module-contributed member does not
// silently lose it.
func mergeTypeMembers(into, from *introspection.Type) {
	fields := map[string]bool{}
	for _, field := range into.Fields {
		fields[field.Name] = true
	}
	for _, field := range from.Fields {
		if !fields[field.Name] {
			into.Fields = append(into.Fields, field)
		}
	}

	inputs := map[string]bool{}
	for _, input := range into.InputFields {
		inputs[input.Name] = true
	}
	for _, input := range from.InputFields {
		if !inputs[input.Name] {
			into.InputFields = append(into.InputFields, input)
		}
	}

	values := map[string]bool{}
	for _, value := range into.EnumValues {
		values[value.Name] = true
	}
	for _, value := range from.EnumValues {
		if !values[value.Name] {
			into.EnumValues = append(into.EnumValues, value)
		}
	}
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

// selfContribution extracts what a module contributes to its own client schema:
// the entry points on the extendable types, plus the types those reach.
//
// Include cannot do this alone. It selects a module's types by their type-level
// sourceMap directive, and a module's own types do not carry one in its own
// client schema — only the Query field that reaches them does. So the fields
// come from Include and the types come from walking out of them.
//
// The walk is bounded by what the merge does with the result: mergeSchemas only
// adds types the module-facing schema lacks, so anything core the walk passes
// through is already there and is dropped.
func selfContribution(schema *introspection.Schema, moduleName string) *introspection.Schema {
	contributed := schema.Include(moduleName)

	byName := map[string]*introspection.Type{}
	for _, typ := range schema.Types {
		byName[typ.Name] = typ
	}

	seen := map[string]bool{}
	var walk func(ref *introspection.TypeRef)
	walk = func(ref *introspection.TypeRef) {
		for ; ref != nil; ref = ref.OfType {
			if ref.Name == "" || seen[ref.Name] {
				continue
			}
			typ, ok := byName[ref.Name]
			if !ok {
				continue
			}
			seen[ref.Name] = true
			contributed.Types = append(contributed.Types, typ)
			for _, field := range typ.Fields {
				walk(field.TypeRef)
				for _, arg := range field.Args {
					walk(arg.TypeRef)
				}
			}
			for _, input := range typ.InputFields {
				walk(input.TypeRef)
			}
		}
	}

	for _, typ := range contributed.Types {
		for _, field := range typ.Fields {
			walk(field.TypeRef)
			for _, arg := range field.Args {
				walk(arg.TypeRef)
			}
		}
	}

	return contributed
}
