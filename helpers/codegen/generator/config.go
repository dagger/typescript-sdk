package generator

// Config drives one codegen run. Exactly one of ModuleConfig / ClientConfig /
// EntrypointConfig is set (none of them means library mode); that choice selects
// the output file names and how the generated bindings import the SDK runtime.
type Config struct {
	// OutputDir is the path to put the generated code.
	OutputDir string

	// ModuleConfig is the specific config to generate a module's own bindings.
	ModuleConfig *ModuleGeneratorConfig

	// ClientConfig is the specific config to generate standalone client.
	ClientConfig *ClientGeneratorConfig

	// EntrypointConfig is the specific config to generate the static dispatch
	// entrypoint file.
	EntrypointConfig *EntrypointGeneratorConfig

	// DangEntrypointConfig is the specific config to generate the Dang module
	// entrypoint the engine loads under a manifest `[entrypoint]` table.
	DangEntrypointConfig *DangEntrypointGeneratorConfig
}

// Specific configuration for module generation.
type ModuleGeneratorConfig struct {
	// Name of the module to generate code for. Its own types stay in the core
	// file; only its dependencies are split into per-module files.
	ModuleName string

	// BoundModules gives, per module name, the source a generated client serves
	// on use: its git ref+pin or its workspace-relative path. A module client
	// with an entry serves that module before its first query, so a client used
	// outside the dispatcher still resolves. Modules with no entry (e.g. a
	// manifest dependency the engine already serves) get no serve hook.
	BoundModules []BoundModule
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

// BoundModule identifies one module a generated client serves. The generated
// serveBoundModule bootstrap uses Kind to decide how to load it at runtime: a
// local module (LOCAL_SOURCE/DIR_SOURCE) is resolved against the workspace by
// its workspace-root-relative Path
// (dag.currentWorkspace().moduleSource(Path)); a git module (GIT_SOURCE) is
// served from its canonical Ref + Pin, which resolve from anywhere.
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
}

// Specific configuration for client generation.
type ClientGeneratorConfig struct {
	// The name of the module to generate for.
	ModuleName string

	// BoundModules are the modules the generated client serves; they drive the
	// generated serveBoundModule bootstrap. A client is generated per SDK scope
	// rather than per module, so one package can serve several — the core API
	// they share is emitted once.
	BoundModules []BoundModule

	// The directory where the client will be generated.
	ClientDir string

	// The engine version from dagger.json, used to pin the dagger.io/dagger dependency.
	// This is only populated when generating from a module source (not in tests).
	EngineVersion string
}
