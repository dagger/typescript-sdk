package templates

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"text/template"
)

// DangEntrypointOptions controls the container recipe baked into the generated
// entrypoint's call().
//
// Every value is resolved once, at generation. The emitted Dang carries answers,
// not rules: it never re-derives the JS runtime, the image or the mount layout.
// Changing any of them means re-running `dagger generate`.
type DangEntrypointOptions struct {
	// ModuleName is the module's name as the workspace records it, used in the
	// error a missing generated file raises.
	ModuleName string

	// Runtime selects the container recipe: "node", "bun" or "deno".
	Runtime string

	// ModulePath is the module directory relative to the workspace root, used
	// as the container workdir. "." when the module is the workspace root.
	ModulePath string

	// DispatchFile is the generated dispatcher call() execs, relative to the
	// module directory.
	DispatchFile string

	// TSConfigPath is the tsconfig tsx loads, relative to the module directory.
	// Node only.
	TSConfigPath string

	// ClientsDir is the workspace-root-relative directory holding the shared
	// client packages — one directory per module named exactly like its
	// @dagger.io/* package, the library among them as dagger/. When set, the
	// recipe mounts it whole at node_modules/@dagger.io instead of mounting
	// the module's own sdk/, and the generated-file check looks there for the
	// library and beside the dispatcher for the loader. Empty keeps the
	// embedded layout.
	ClientsDir string
}

// Pins shared with the engine's built-in TypeScript runtime
// (sdk/typescript/runtime/tsdistconsts/consts.go). A generated entrypoint has no
// engine assets, so it names the same images directly.
const (
	dangNodeImageRef = "node:24.13.1-alpine@sha256:4f696fbf39f383c1e486030ba6b289a5d9af541642fc78ab197e584a113b9c03"
	dangBunImageRef  = "oven/bun:1.3.0-alpine@sha256:37e6b1cbe053939bccf6ae4507977ed957eaa6e7f275670b72ad6348e0d2c11f"
	dangDenoImageRef = "denoland/deno:alpine-2.5.0@sha256:8f58f398552de8ee5028b69bd92370d0703bcec220adcfc68a07669f1be241f3"

	// runtime_node.go mounts tsx out of the engine image; a generated entrypoint
	// has no engine assets, so it installs tsx before anything module-specific
	// and the layer keys on (image digest, tsx version) alone.
	dangTsxVersion = "4.22.4"

	// The directory the SDK bundle is generated into, mounted as the
	// @dagger.io/dagger package for node and bun.
	dangSDKDir = "sdk"
)

// DefaultDispatchFile is the filename of the generated dispatcher the Dang
// entrypoint's call() execs. It sits beside the legacy __dagger.entrypoint.ts
// rather than replacing it, so a module keeps working on an engine that still
// loads it through the built-in runtime.
const DefaultDispatchFile = "__dagger.dispatch.ts"

// Indentation of the generated Dang, baked here rather than in the template:
// the shape is fixed, and rendering whole chains in Go keeps the output stable
// without whitespace control in every template action.
const (
	dangTypeIndent    = "        "     // a type's own builder chain
	dangFnIndent      = "          "   // a function expression inside withFunction
	dangFnChainIndent = "            " // that function expression's own chain
)

// DangEntrypointTemplateFuncs returns the template.FuncMap used by
// src/entrypoint_dang/*.gtpl. The Dang expression rendering lives here; the
// template handles only the structural layout.
func DangEntrypointTemplateFuncs(module *TypedefModule, opts DangEntrypointOptions) template.FuncMap {
	c := &dangFuncCtx{module: module, opts: opts}
	return template.FuncMap{
		"dangTypeEntries":          c.dangTypeEntries,
		"dangRuntimeChain":         c.dangRuntimeChain,
		"dangDispatchExec":         c.dangDispatchExec,
		"dangRequireGeneratedBody": c.dangRequireGeneratedBody,
	}
}

type dangFuncCtx struct {
	module *TypedefModule
	opts   DangEntrypointOptions
}

// dangString renders a Dang string literal. Dang has no \u escape, so a control
// character is dropped rather than emitted as an escape the parser would reject;
// they cannot appear in the doc comments and identifiers this reads from.
func dangString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r >= 0x20 {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// dangNamedArgs renders trailing named arguments for a builder call. Keys are
// sorted so the output is stable across runs.
func dangNamedArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, k := range sortedKeys(args) {
		parts = append(parts, fmt.Sprintf("%s: %s", k, args[k]))
	}
	return ", " + strings.Join(parts, ", ")
}

// dangChain joins a head expression and its builder calls, one per line, each
// indented and prefixed with a dot.
func dangChain(head string, calls []string, indent string) string {
	if len(calls) == 0 {
		return head
	}
	return head + "\n" + indent + "." + strings.Join(calls, "\n"+indent+".")
}

// dangSourceMapExpr replays a location captured at generation. Unlike the
// TypeScript renderer's dag.sourceMap(), this is evaluated in the engine, so a
// module keeps its source-map comments in dependents' bindings without a
// container ever starting.
func dangSourceMapExpr(loc *TypedefLocation) string {
	if loc == nil {
		return ""
	}
	return fmt.Sprintf("sourceMap(%s, %d, %d)", dangString(loc.Filepath), loc.Line, loc.Column)
}

// dangTypeDef maps a scanned type onto a Dang TypeDef expression. An OBJECT,
// ENUM or INTERFACE renders as a name-only reference; the engine binds it by
// kind and original name to the full definition types() returns.
func (c *dangFuncCtx) dangTypeDef(t *TypedefType) string {
	if t == nil {
		return "typeDef"
	}
	switch t.Kind {
	case KindScalar:
		return fmt.Sprintf("typeDef.withScalar(%s)", dangString(t.Name))
	case KindObject:
		return fmt.Sprintf("typeDef.withObject(%s)", dangString(t.Name))
	case KindList:
		return fmt.Sprintf("typeDef.withListOf(%s)", c.dangTypeDef(t.TypeDef))
	case KindVoid:
		return fmt.Sprintf("typeDef.withKind(TypeDefKind.%s).withOptional(true)", KindVoid)
	case KindEnum:
		return fmt.Sprintf("typeDef.withEnum(%s)", dangString(t.Name))
	case KindInterface:
		return fmt.Sprintf("typeDef.withInterface(%s)", dangString(t.Name))
	case KindString, KindInteger, KindFloat, KindBoolean:
		return fmt.Sprintf("typeDef.withKind(TypeDefKind.%s)", t.Kind)
	default:
		return "typeDef"
	}
}

// ---- type definitions ------------------------------------------------------

// dangTypeEntries renders every type the module defines as one element of the
// list types() returns: objects, then enums, then interfaces, each sorted by
// name so regeneration is stable.
func (c *dangFuncCtx) dangTypeEntries() []string {
	entries := make([]string, 0, len(c.module.Objects)+len(c.module.Enums)+len(c.module.Interfaces))
	for _, name := range sortedObjectKeys(c.module.Objects) {
		entries = append(entries, c.dangObjectEntry(c.module.Objects[name]))
	}
	for _, name := range sortedEnumKeys(c.module.Enums) {
		entries = append(entries, c.dangEnumEntry(c.module.Enums[name]))
	}
	for _, name := range sortedInterfaceKeys(c.module.Interfaces) {
		entries = append(entries, c.dangInterfaceEntry(c.module.Interfaces[name]))
	}
	return entries
}

func (c *dangFuncCtx) dangObjectEntry(obj *TypedefObject) string {
	calls := []string{c.dangObjectDef(obj)}

	for _, name := range sortedFunctionKeys(obj.Methods) {
		calls = append(calls, c.dangWrappedCall("withFunction", c.dangFunctionExpr(obj.Methods[name])))
	}
	for _, name := range sortedPropertyKeys(obj.Properties) {
		if prop := obj.Properties[name]; prop.IsExposed {
			calls = append(calls, c.dangFieldCall(prop))
		}
	}
	// A plain object with no constructor is registered as-is; the engine treats
	// the object that does declare one as the module's main object.
	if obj.Constructor != nil {
		calls = append(calls, c.dangWrappedCall("withConstructor", c.dangConstructorExpr(obj)))
	}
	return dangChain("typeDef", calls, dangTypeIndent)
}

func (c *dangFuncCtx) dangEnumEntry(e *TypedefEnum) string {
	calls := []string{c.dangEnumDef(e)}
	for _, name := range sortedEnumValueKeys(e.Values) {
		calls = append(calls, c.dangEnumMemberCall(e.Values[name]))
	}
	return dangChain("typeDef", calls, dangTypeIndent)
}

func (c *dangFuncCtx) dangInterfaceEntry(iface *TypedefInterface) string {
	calls := []string{c.dangInterfaceDef(iface)}
	for _, name := range sortedFunctionKeys(iface.Functions) {
		calls = append(calls, c.dangWrappedCall("withFunction", c.dangFunctionExpr(iface.Functions[name])))
	}
	return dangChain("typeDef", calls, dangTypeIndent)
}

// dangWrappedCall puts a function expression on its own indented lines inside
// withFunction(...) / withConstructor(...), which keeps the enclosing type's
// chain readable however long the function is.
func (c *dangFuncCtx) dangWrappedCall(name, expr string) string {
	return fmt.Sprintf("%s(\n%s%s,\n%s)", name, dangFnIndent, expr, dangTypeIndent)
}

func (c *dangFuncCtx) dangObjectDef(obj *TypedefObject) string {
	args := map[string]string{}
	if obj.Description != "" {
		args["description"] = dangString(obj.Description)
	}
	if obj.Deprecated != "" {
		args["deprecated"] = dangString(obj.Deprecated)
	}
	if sm := dangSourceMapExpr(obj.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withObject(%s%s)", dangString(obj.Name), dangNamedArgs(args))
}

func (c *dangFuncCtx) dangFieldCall(prop *TypedefProperty) string {
	args := map[string]string{}
	if prop.Description != "" {
		args["description"] = dangString(prop.Description)
	}
	if prop.Deprecated != "" {
		args["deprecated"] = dangString(prop.Deprecated)
	}
	if sm := dangSourceMapExpr(prop.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withField(%s, %s%s)",
		dangString(propFieldName(prop)), c.dangTypeDef(prop.Type), dangNamedArgs(args))
}

func (c *dangFuncCtx) dangEnumDef(e *TypedefEnum) string {
	args := map[string]string{}
	if e.Description != "" {
		args["description"] = dangString(e.Description)
	}
	if sm := dangSourceMapExpr(e.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withEnum(%s%s)", dangString(e.Name), dangNamedArgs(args))
}

func (c *dangFuncCtx) dangEnumMemberCall(v *TypedefEnumValue) string {
	args := map[string]string{"value": dangString(v.Value)}
	if v.Description != "" {
		args["description"] = dangString(v.Description)
	}
	if v.Deprecated != "" {
		args["deprecated"] = dangString(v.Deprecated)
	}
	if sm := dangSourceMapExpr(v.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withEnumMember(%s%s)", dangString(v.Name), dangNamedArgs(args))
}

func (c *dangFuncCtx) dangInterfaceDef(iface *TypedefInterface) string {
	args := map[string]string{}
	if iface.Description != "" {
		args["description"] = dangString(iface.Description)
	}
	if sm := dangSourceMapExpr(iface.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withInterface(%s%s)", dangString(iface.Name), dangNamedArgs(args))
}

func (c *dangFuncCtx) dangArgCall(arg *TypedefArgument) string {
	args := map[string]string{}
	if arg.Description != "" {
		args["description"] = dangString(arg.Description)
	}
	if arg.Deprecated != "" {
		args["deprecated"] = dangString(arg.Deprecated)
	}
	if arg.DefaultPath != "" {
		args["defaultPath"] = dangString(arg.DefaultPath)
	}
	if arg.DefaultAddress != "" {
		args["defaultAddress"] = dangString(arg.DefaultAddress)
	}
	if len(arg.Ignore) > 0 {
		ignores := make([]string, len(arg.Ignore))
		for i, p := range arg.Ignore {
			ignores[i] = dangString(p)
		}
		args["ignore"] = "[" + strings.Join(ignores, ", ") + "]"
	}

	td := c.dangTypeDef(arg.Type)
	if arg.IsOptional {
		td += ".withOptional(true)"
	}
	// An explicit `null` default still registers as the arg's default, unlike the
	// dispatcher's hasDefault, which excludes null on purpose.
	if len(arg.DefaultValue) > 0 {
		dv, ok := c.resolveDangDefaultValue(arg)
		if !ok {
			if !arg.IsOptional {
				td += ".withOptional(true)"
			}
		} else if encoded, err := json.Marshal(dv); err == nil {
			// Dang has two JSON types and they do not convert: the builtin `JSON`
			// module, and `Dagger.JSON`, the schema's scalar. A schema field such as
			// withArg's defaultValue wants the qualified one. (call()'s own return
			// stays bare `JSON!`, because that is what the engine writes into the
			// ModuleEntrypoint interface it injects.) It is also nullable, so the
			// cast is `Dagger.JSON`, not `Dagger.JSON!`.
			args["defaultValue"] = fmt.Sprintf("(%s :: Dagger.JSON)", dangString(string(encoded)))
		}
	}
	if sm := dangSourceMapExpr(arg.Location); sm != "" {
		args["sourceMap"] = sm
	}
	return fmt.Sprintf("withArg(%s, %s%s)", dangString(arg.Name), td, dangNamedArgs(args))
}

// resolveDangDefaultValue mirrors the TypeScript renderer: only a primitive
// default survives, and an enum default is recorded by member name rather than
// by its wire value.
func (c *dangFuncCtx) resolveDangDefaultValue(arg *TypedefArgument) (any, bool) {
	if !isPrimitive(arg.Type) {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(arg.DefaultValue, &v); err != nil {
		return nil, false
	}
	if arg.Type.Kind != KindEnum {
		return v, true
	}
	e, ok := c.module.Enums[arg.Type.Name]
	if !ok {
		return v, true
	}
	for _, name := range sortedEnumValueKeys(e.Values) {
		val := e.Values[name]
		if val.Value == fmt.Sprintf("%v", v) {
			return val.Name, true
		}
	}
	return nil, false
}

// dangFunctionExpr turns a scanned function into the Dang expression declaring
// it. It is the same translation the TypeScript renderer's renderFunctionExpr
// does, from the same typedef, and nothing enforces that the two agree — a new
// decorator has to be taught to both.
func (c *dangFuncCtx) dangFunctionExpr(fn *TypedefFunction) string {
	name := fn.Alias
	if name == "" {
		name = fn.Name
	}
	head := fmt.Sprintf("function(%s, %s)", dangString(name), c.dangTypeDef(fn.ReturnType))

	var calls []string
	if fn.Description != "" {
		calls = append(calls, fmt.Sprintf("withDescription(%s)", dangString(fn.Description)))
	}
	if sm := dangSourceMapExpr(fn.Location); sm != "" {
		calls = append(calls, fmt.Sprintf("withSourceMap(%s)", sm))
	}
	for _, arg := range fn.Arguments {
		calls = append(calls, c.dangArgCall(arg))
	}
	switch fn.Cache {
	case "never":
		calls = append(calls, "withCachePolicy(FunctionCachePolicy.Never)")
	case "session":
		calls = append(calls, "withCachePolicy(FunctionCachePolicy.PerSession)")
	case "", "default":
	default:
		calls = append(calls, fmt.Sprintf(
			"withCachePolicy(FunctionCachePolicy.Default, timeToLive: %s)", dangString(fn.Cache)))
	}
	if fn.Deprecated != "" {
		calls = append(calls, fmt.Sprintf("withDeprecated(reason: %s)", dangString(fn.Deprecated)))
	}
	if fn.IsCheck {
		calls = append(calls, "withCheck")
	}
	if fn.IsGenerator {
		calls = append(calls, "withGenerator")
	}
	if fn.IsUp {
		calls = append(calls, "withUp")
	}
	if fn.IsAgent {
		calls = append(calls, "withAgent")
	}
	return dangChain(head, calls, dangFnChainIndent)
}

// dangConstructorExpr renders an object's constructor. Its return type is a
// name-only reference to the owning object rather than the accumulated
// definition: the engine binds it by kind and original name, which lets the
// whole object stay one chain.
func (c *dangFuncCtx) dangConstructorExpr(obj *TypedefObject) string {
	head := fmt.Sprintf("function(\"\", typeDef.withObject(%s))", dangString(obj.Name))

	var calls []string
	if obj.Constructor != nil {
		for _, arg := range obj.Constructor.Arguments {
			calls = append(calls, c.dangArgCall(arg))
		}
	}
	return dangChain(head, calls, dangFnChainIndent)
}

// ---- the call() container recipe -------------------------------------------

func (c *dangFuncCtx) modulePath() string {
	p := c.opts.ModulePath
	if p == "" {
		p = "."
	}
	return path.Clean(p)
}

// workdir is where the module tree lands inside the container. The module's path
// is baked here rather than read from workspace.cwd: cwd is the caller's, and a
// module has to run from its own directory whoever called it.
func (c *dangFuncCtx) workdir() string {
	if p := c.modulePath(); p != "." {
		return "/workspace/" + p
	}
	return "/workspace"
}

func (c *dangFuncCtx) dispatchFile() string {
	if c.opts.DispatchFile == "" {
		return DefaultDispatchFile
	}
	return c.opts.DispatchFile
}

// moduleName falls back to the typedef's name only so a hand-run codegen still
// produces something readable; the SDK always passes the real one.
func (c *dangFuncCtx) moduleName() string {
	if c.opts.ModuleName != "" {
		return c.opts.ModuleName
	}
	return c.module.Name
}

func (c *dangFuncCtx) tsConfigPath() string {
	if c.opts.TSConfigPath == "" {
		return "tsconfig.json"
	}
	return c.opts.TSConfigPath
}

// dangRequiredFiles lists the generated files call() cannot run without, in the
// order a reader would miss them: the dispatcher first, then the package it
// imports, then the config the loader reads.
//
// Only what this module's own recipe actually mounts or execs. Deno resolves
// @dagger.io/dagger through deno.json rather than node_modules, and bun needs no
// tsconfig because it runs the dispatcher directly.
func (c *dangFuncCtx) dangRequiredFiles() []string {
	files := []string{c.dispatchFile()}
	if c.opts.ClientsDir == "" {
		files = append(files,
			dangSDKDir+"/index.ts",
			dangSDKDir+"/client.gen.ts",
			dangSDKDir+"/core.js",
		)
	} else {
		// The loader is the one binding still inside the module under the
		// shared layout; the library sits in the shared directory, checked
		// separately against the workspace root.
		files = append(files, "loader.gen.ts")
	}
	switch c.opts.Runtime {
	case "deno":
		files = append(files, "deno.json")
	case "bun":
	default:
		files = append(files, c.tsConfigPath())
	}
	return files
}

// dangRequiredSharedFiles lists what call() needs from the shared client
// directory, relative to it. Only under the shared layout.
func (c *dangFuncCtx) dangRequiredSharedFiles() []string {
	if c.opts.ClientsDir == "" {
		return nil
	}
	return []string{
		"dagger/index.ts",
		"dagger/client.gen.ts",
		"dagger/core.js",
	}
}

// dangRequireGeneratedBody renders the body of requireGenerated: one existence
// check per required file, chained so the first missing one names itself.
//
// Rendered here rather than in the template because the file list is known at
// generation and an if/else-if chain is easier to read as flat output than as
// nested template actions.
func (c *dangFuncCtx) dangRequireGeneratedBody() string {
	dir := "/"
	if p := c.modulePath(); p != "." {
		dir = "/" + p
	}

	var b strings.Builder
	fmt.Fprintf(&b, "let dir = workspace.directory(%s)\n", dangString(dir))
	if len(c.dangRequiredSharedFiles()) > 0 {
		fmt.Fprintf(&b, "    let clients = workspace.directory(%s)\n", dangString("/"+c.opts.ClientsDir))
	}
	b.WriteString("    ")

	for _, file := range c.dangRequiredFiles() {
		fmt.Fprintf(&b, "if (dir.exists(%s) == false) {\n", dangString(file))
		fmt.Fprintf(&b, "      raise %s\n", dangString(fmt.Sprintf(
			"module %q cannot run: the generated file %q is missing. "+
				"Run `dagger generate` and commit what it writes.",
			c.moduleName(), file)))
		b.WriteString("    } else ")
	}
	for _, file := range c.dangRequiredSharedFiles() {
		fmt.Fprintf(&b, "if (clients.exists(%s) == false) {\n", dangString(file))
		fmt.Fprintf(&b, "      raise %s\n", dangString(fmt.Sprintf(
			"module %q cannot run: the generated file %q is missing. "+
				"Run `dagger generate` and commit what it writes.",
			c.moduleName(), c.opts.ClientsDir+"/"+file)))
		b.WriteString("    } else ")
	}
	b.WriteString("{\n      null\n    }")

	return b.String()
}

// dangRuntimeChain renders the body of the private runtime() helper: everything
// up to but not including the per-call exec, so the whole build is shared across
// calls and across modules that share a prefix.
func (c *dangFuncCtx) dangRuntimeChain() string {
	var calls []string

	switch c.opts.Runtime {
	case "bun":
		calls = append(calls, fmt.Sprintf("from(%s)", dangString(dangBunImageRef)))
	case "deno":
		calls = append(calls, fmt.Sprintf("from(%s)", dangString(dangDenoImageRef)))
	default:
		calls = append(calls,
			fmt.Sprintf("from(%s)", dangString(dangNodeImageRef)),
			`withExec(["apk", "add", "--no-cache", "ca-certificates"])`,
			`withEnvVariable("NODE_OPTIONS", "--use-openssl-ca")`,
			fmt.Sprintf(`withExec(["npm", "install", "-g", %s])`, dangString("tsx@"+dangTsxVersion)),
		)
	}

	// node_modules is excluded rather than mounted: it is a host build artifact
	// that can dwarf the source, and for node and bun the one package the module
	// actually needs is mounted explicitly just below.
	calls = append(calls,
		`withMountedDirectory("/workspace", workspace.directory("/", exclude: ["**/node_modules"]))`,
		fmt.Sprintf("withWorkdir(%s)", dangString(c.workdir())),
	)

	// Deno resolves @dagger.io/dagger through the import map in deno.json, so only
	// node and bun need the packages mounted where module resolution looks for
	// them. The shared client directory maps 1:1 onto the @dagger.io scope —
	// one package directory per module, the library as dagger/ — so one mount
	// of the whole directory covers the library and every client.
	if c.opts.Runtime != "deno" {
		if c.opts.ClientsDir != "" {
			calls = append(calls, fmt.Sprintf(
				`withMountedDirectory("node_modules/@dagger.io", workspace.directory(%s))`,
				dangString("/"+c.opts.ClientsDir)))
		} else {
			sdkPath := dangSDKDir
			if p := c.modulePath(); p != "." {
				sdkPath = path.Join(p, dangSDKDir)
			}
			calls = append(calls, fmt.Sprintf(
				`withMountedDirectory("node_modules/@dagger.io/dagger", workspace.directory(%s))`,
				dangString("/"+sdkPath)))
		}
	}

	return dangChain("container", calls, "      ")
}

// dangDispatchExec renders the argv that runs the generated dispatcher.
func (c *dangFuncCtx) dangDispatchExec() string {
	file := c.dispatchFile()

	var argv []string
	switch c.opts.Runtime {
	case "bun":
		argv = []string{"bun", "run", file, "engine-call"}
	case "deno":
		argv = []string{"deno", "run", "-q", "-A", file, "engine-call"}
	default:
		tsconfig := c.opts.TSConfigPath
		if tsconfig == "" {
			tsconfig = "tsconfig.json"
		}
		argv = []string{"tsx", "--no-deprecation", "--tsconfig", tsconfig, file, "engine-call"}
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = dangString(a)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
