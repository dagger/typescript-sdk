package generator

// Config drives one codegen run. ModuleConfig covers both a module's own
// bindings and a standalone client scope — they render the same client files
// against the same vendored library, differing only in layout (EmitLoader /
// FlatClients). None of the *Config fields set means library mode.
type Config struct {
	// OutputDir is the path to put the generated code.
	OutputDir string

	// ModuleConfig is the config to generate a scope's client bindings: a
	// module's own or a standalone client's.
	ModuleConfig *ModuleGeneratorConfig

	// EntrypointConfig is the specific config to generate the static dispatch
	// entrypoint file.
	EntrypointConfig *EntrypointGeneratorConfig

	// DangEntrypointConfig is the specific config to generate the Dang module
	// entrypoint the engine loads under a manifest `[entrypoint]` table.
	DangEntrypointConfig *DangEntrypointGeneratorConfig
}

// ModuleGeneratorConfig drives a scope's client generation — the same for a
// module's own bindings and a standalone client. The output is one core
// client.gen.ts (the vendored library's bindings) plus one <module>.gen.ts
// client per module, each serving its own module on use.
type ModuleGeneratorConfig struct {
	// Name of the module to generate code for, when this is a module's own
	// bindings. Empty for a standalone client scope, which has no module of its
	// own — only external targets.
	ModuleName string

	// BoundModules gives, per module name, the source a generated client serves
	// on use: its git ref+pin or its workspace-relative path. A module client
	// with an entry serves that module before its first query. Modules with no
	// entry (e.g. a manifest dependency the engine already serves) get no serve
	// hook.
	BoundModules []BoundModule

	// EmitLoader writes the entrypoint object loader beside the clients. Only a
	// module scope has an entrypoint that needs it; a standalone client omits it.
	EmitLoader bool

	// FlatClients writes the per-module client files in the output root rather
	// than a clients/ subdirectory. A standalone client scope is nothing but its
	// clients, so they sit flat; a module keeps them under clients/, apart from
	// its src/, entrypoint and sdk/.
	FlatClients bool

	// PackagedClients makes a client file reach the library and its siblings by
	// relative path rather than by package name, for a rendering the caller
	// will split into one package directory per module.
	//
	// Bare specifiers cannot carry that layout. npm links a `file:` package as
	// a symlink, and node resolves from the real path — so a client installed
	// into a consumer's node_modules looks for its siblings beside its own
	// directory in the shared tree, not beside the symlink, and finds nothing.
	// Relative imports resolve the same wherever the tree is reached from, and
	// the package names stay the user-facing surface.
	PackagedClients bool
}

// Module-source kinds a generated client can bind to. A local module
// (LOCAL_SOURCE, or DIR_SOURCE — how a workspace-local module resolves in
// practice) is served by its workspace-relative path; a GIT_SOURCE module is
// served from its canonical ref + pin.
const (
	ModuleKindGit   = "GIT_SOURCE"
	ModuleKindLocal = "LOCAL_SOURCE"
	ModuleKindDir   = "DIR_SOURCE"
)

// BoundModule identifies one module a generated client serves. Its serve uses
// Kind to decide how: a local module (LOCAL_SOURCE/DIR_SOURCE) resolves against
// the workspace by its workspace-root-relative Path
// (currentWorkspace().moduleSource(Path)); a git module (GIT_SOURCE) serves from
// its canonical Ref + Pin, which resolve from anywhere.
type BoundModule struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	Ref  string `json:"ref"`
	Pin  string `json:"pin"`
}

// Specific configuration for entrypoint generation.
type EntrypointGeneratorConfig struct {
	// TypedefJSONPath is the path to the JSON-serialized DaggerModule typedef
	// produced by the SDK introspector (e.g. ts-introspector with
	// EMIT_TYPEDEF_JSON_FILE).
	TypedefJSONPath string

	// OutputFile is the filename (relative to OutputDir) where the generated
	// entrypoint source is written. Defaults to "__dagger.entrypoint.ts" for
	// the TypeScript SDK.
	OutputFile string

	// ModuleRoot is the absolute path of the user's module root, used to
	// resolve relative source-import paths for each registered @object class.
	ModuleRoot string

	// SDKImportPath is the bare specifier the entrypoint uses to import
	// runtime helpers (defaults to "@dagger.io/dagger" for TypeScript).
	SDKImportPath string

	// SourceDir is the user's source directory name relative to ModuleRoot
	// (defaults to "src" for TypeScript).
	SourceDir string

	// DispatchMode renders the manifest-v2 dispatcher instead of the legacy
	// entrypoint: one JSON request on stdin, one JSON result on stdout, and no
	// register(). See EntrypointOptions.DispatchMode.
	DispatchMode bool

	// LoaderImportPath is where the generated entrypoint imports the object
	// loader from, relative to the module root. Empty keeps the embedded
	// layout's clients/loader.gen.js.
	LoaderImportPath string
}

// Specific configuration for generating the Dang module entrypoint — the program
// the engine loads under a manifest `[entrypoint]` table.
type DangEntrypointGeneratorConfig struct {
	// TypedefJSONPath is the path to the JSON-serialized DaggerModule typedef
	// produced by the SDK introspector, the same input the dispatcher is
	// rendered from.
	TypedefJSONPath string

	// OutputFile is the path (relative to OutputDir) of the generated program.
	// Defaults to "entrypoint/main.dang".
	OutputFile string

	// ModuleName is the module's name as the workspace records it, used in the
	// error a missing generated file raises. The typedef JSON carries the
	// pascalized *object* name, which is not the same thing.
	ModuleName string

	// Runtime selects the container recipe call() bakes: "node", "bun" or
	// "deno". Defaults to node.
	Runtime string

	// ModulePath is the module directory relative to the workspace root. The
	// recipe uses it as the container workdir, so it is baked at generation
	// rather than read from workspace.cwd, which is the caller's.
	ModulePath string

	// DispatchFile is the dispatcher call() execs, relative to the module
	// directory. Defaults to "__dagger.dispatch.ts".
	DispatchFile string

	// TSConfigPath is the tsconfig tsx loads, relative to the module directory.
	// Node only; defaults to "tsconfig.json".
	TSConfigPath string

	// ClientsDir is the workspace-root-relative directory holding the shared
	// client packages (one per module, the library among them as dagger/).
	// When set, the recipe mounts it at node_modules/@dagger.io instead of the
	// module's own sdk/, and the generated-file check looks there for the
	// library. Empty keeps the embedded sdk/ layout.
	ClientsDir string
}
