package templates

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
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

	// ModulePath is the module directory relative to the workspace root, which
	// is what Include is rebased against. The container workdir comes from
	// currentModule.sourceSubpath instead — the module's path inside its
	// context, which is not the same thing when the context root is not the
	// workspace root.
	ModulePath string

	// PackageManager installs the module's dependencies: "npm", "yarn" or
	// "pnpm" under node, "bun" or "deno" under their own runtimes. Empty falls
	// back to the runtime's own manager, which is the only one its base image
	// is guaranteed to ship.
	PackageManager string

	// PackageManagerVersion pins that manager, as the SDK detected it. Empty
	// uses whatever the base image ships.
	PackageManagerVersion string

	// BaseImage replaces the image the recipe builds on, as the module's
	// `dagger.baseImage` records it. Empty keeps the pin for Runtime.
	//
	// Nothing else in the recipe moves with it: the node recipe still installs
	// ca-certificates with apk and the package manager with npm, so an image
	// that ships neither fails exactly where the engine's builtin runtime fails
	// on the same image.
	BaseImage string

	// ClientVersion is the engine release the module's bindings are generated
	// for, declared on the exec that starts the module process.
	ClientVersion string

	// DispatchFile is the generated dispatcher call() execs, relative to the
	// module directory.
	DispatchFile string

	// TSConfigPath is the tsconfig tsx loads, relative to the module directory.
	// Node only.
	TSConfigPath string

	// Include lists paths outside the module directory the module builds and
	// runs with, workspace-relative, as the SDK derived them from what the
	// module's code imports. A "!"-prefixed entry excludes. Rendered as the
	// reads the `sources` member makes, rebased to the module-relative form that
	// member resolves in.
	Include []string

	// Names are the engine's names for the module's declarations
	// (ModuleSource.naming): what decides which object carries the
	// constructor.
	Names *ModuleNames
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

	// npm bundled in dangNodeImageRef. The SDK resolves a lockfile-detected npm
	// to this same version, so the usual case needs no global install at all —
	// only an explicit pin that disagrees with the image does.
	dangNodeNpmVersion = "11.8.0"

	// The directory the SDK bundle is generated into, mounted as the
	// @dagger.io/dagger package for node and bun.
	dangSDKDir = "sdk"

	// Where dependencies are installed, and where the module context is mounted.
	// The install has to land outside the context mount or the mount would hide
	// it, and outside "/" because npm refuses to install at the filesystem root
	// ("Tracker \"idealTree\" already exists").
	dangInstallDir = "/deps"
	dangContextDir = "/workspace"
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
		"dangBaseChain":            c.dangBaseChain,
		"dangDependenciesChain":    c.dangDependenciesChain,
		"dangRuntimeChain":         c.dangRuntimeChain,
		"dangDispatchExec":         c.dangDispatchExec,
		"dangRequireGeneratedBody": c.dangRequireGeneratedBody,
		"dangSourcesMember":        c.dangSourcesMember,
		"dangClientVersion":        c.dangClientVersion,
	}
}

// dangClientVersion renders the release the module process is told its
// bindings are generated for.
func (c *dangFuncCtx) dangClientVersion() string {
	return dangString(c.opts.ClientVersion)
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
	if obj.IsCollection {
		calls = append(calls, "withCollection")
	}

	for _, name := range sortedFunctionKeys(obj.Methods) {
		method := obj.Methods[name]
		calls = append(calls, c.dangWrappedCall("withFunction", c.dangFunctionExpr(method)))
		if method.IsCollectionGet {
			member := method.Name
			if method.Alias != "" {
				member = method.Alias
			}
			calls = append(calls, fmt.Sprintf("withCollectionGet(%s)", dangString(member)))
		}
	}
	for _, name := range sortedPropertyKeys(obj.Properties) {
		if prop := obj.Properties[name]; prop.IsExposed {
			calls = append(calls, c.dangFieldCall(prop))
			if prop.IsCollectionKeys {
				calls = append(calls, fmt.Sprintf("withCollectionKeys(%s)", dangString(propFieldName(prop))))
			}
			if prop.IsCollectionDelta {
				calls = append(calls, fmt.Sprintf("withCollectionDelta(%s)", dangString(propFieldName(prop))))
			}
		}
	}
	// The engine identifies an entrypoint module's main object by its
	// constructor typedef alone — no name fallback, and none of the default
	// synthesis runtime modules get. The main object therefore always carries
	// one, defaulted to no-arg, or the module never surfaces in the workspace;
	// and no other object may, or the engine rejects the whole module with
	// "multiple object constructors are not supported". A secondary class's own
	// TypeScript constructor is still called — by the module's own code, which
	// the dispatcher's `case ""` covers whatever the typedef says.
	if c.isMainObject(obj) {
		calls = append(calls, c.dangWrappedCall("withConstructor", c.dangConstructorExpr(obj)))
	}
	return dangChain("typeDef", calls, dangTypeIndent)
}

// isMainObject decides which object carries the constructor. The engine takes
// whichever object of an entrypoint module carries one for its main object, so
// the choice is this generator's: the object the engine would take for the
// main one by name in a module whose runtime registers its objects
// (ModuleNaming.mainObject). Failing that, the object whose name pascalizes
// like the scanner's PascalCase of the module's name, which is how it was
// chosen before, so that a module whose main object only that rule found
// keeps its constructor. One object at most either way.
func (c *dangFuncCtx) isMainObject(obj *TypedefObject) bool {
	return obj.Name == c.mainObjectName()
}

// mainObjectName is the name of the object isMainObject picks, or "".
func (c *dangFuncCtx) mainObjectName() string {
	if c.opts.Names != nil {
		if _, ok := c.module.Objects[c.opts.Names.MainObject]; ok {
			return c.opts.Names.MainObject
		}
	}
	for _, name := range sortedObjectKeys(c.module.Objects) {
		if pascalize(name) == pascalize(c.module.Name) {
			return name
		}
	}
	return ""
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
	returnType := c.dangTypeDef(fn.ReturnType)
	if fn.IsNullable {
		returnType += ".withOptional(true)"
	}
	head := fmt.Sprintf("function(%s, %s)", dangString(name), returnType)

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

// dangSourcesMember renders the `sources` member: the directory the engine
// merges into the module context before anything reads the module digest.
//
// Always emitted, even for a module that needs nothing outside its own
// directory. One shape for every module is the engine's own choice, and it is
// also what decides the interface: an entrypoint that declares `sources` gets a
// `types` and `call` with no workspace argument, which is the whole point —
// under the old `resources` member they still took one, and that workspace sat
// outside every cache key, so a read through it could be served stale.
//
// Unlike `resources`, this runs with API access and receives a workspace, so it
// returns data rather than a list of patterns for the engine to interpret. The
// workspace is the module's own: a local checkout, the workspace it was loaded
// from, or a Git commit — never the caller's.
//
// Keyed by `workspace.cwd`, which the engine sets to the module's path, so the
// result is rooted at the context root the same way contextDirectory is.
func (c *dangFuncCtx) dangSourcesMember() string {
	var calls []string

	// Outside paths first, so the module's own directory wins a collision — as
	// it does again when the engine overlays the module's real files on top of
	// all this.
	for _, g := range c.dangSourceGroups() {
		quoted := make([]string, len(g.include))
		for i, p := range g.include {
			quoted[i] = dangString(p)
		}
		args := []string{dangString(g.dir), "include: [" + strings.Join(quoted, ", ") + "]"}

		exclude := append([]string{}, g.exclude...)
		exclude = append(exclude, "**/.git", "**/node_modules")
		quotedEx := make([]string, len(exclude))
		for i, p := range exclude {
			quotedEx[i] = dangString(p)
		}
		args = append(args, "exclude: ["+strings.Join(quotedEx, ", ")+"]", "gitignore: true")

		calls = append(calls, fmt.Sprintf("withDirectory(workspace.cwd + %s, workspace.directory(%s))",
			dangString("/"+g.dir), strings.Join(args, ", ")))
	}

	// The module's own directory, read without .gitignore: a module may ignore
	// the generated clients/ it cannot run without, and the engine's own overlay
	// of these files is what the manifest check below reads.
	calls = append(calls, `withDirectory(workspace.cwd, workspace.directory(".", exclude: ["**/.git", "**/node_modules"]))`)

	return fmt.Sprintf(`  """
  Everything this module builds from, as one directory.

  The engine merges it into the module context before anything reads the module
  digest, so a change to any of it gives every call a new cache key — which is
  what a workspace read could never do, the workspace being outside every key.

  The module's own directory always, plus whatever its code imports from outside
  it. Rooted at the context root, which is what workspace.cwd keys each entry to.
  """
  pub sources(workspace: Workspace!): Directory! {
    %s
  }

`, dangChain("directory", calls, "      "))
}

// dangSourceGroup is one workspace read the `sources` member makes: a directory
// to read, the entries to take from it, and what to leave behind.
type dangSourceGroup struct {
	dir     string
	include []string
	exclude []string
}

// dangSourceGroups turns the SDK's workspace-relative include list into the
// reads `sources` makes, module-relative.
//
// Grouped by the parent directory of each pattern, and read with an include
// filter rather than named directly, because a pattern can be a directory, a
// file (`../tsconfig.base.json`) or a glob (`../shared/*.ts`) and the parent
// read spells all three the same way.
func (c *dangFuncCtx) dangSourceGroups() []dangSourceGroup {
	mod := c.modulePath()

	var order []string
	groups := map[string]*dangSourceGroup{}
	group := func(dir string) *dangSourceGroup {
		if g, ok := groups[dir]; ok {
			return g
		}
		g := &dangSourceGroup{dir: dir}
		groups[dir] = g
		order = append(order, dir)
		return g
	}

	// Includes first, so every group exists before an exclude looks for the one
	// it belongs to.
	var excludes []string
	for _, p := range c.opts.Include {
		rel, negated := c.relToModule(p, mod)
		if negated {
			excludes = append(excludes, rel)
			continue
		}
		g := group(path.Dir(rel))
		g.include = append(g.include, path.Base(rel))
	}

	for _, ex := range excludes {
		// Onto every group whose directory contains it, rebased: an exclude is
		// relative to the read it applies to. Dropping a subpath in more reads
		// than strictly needed cannot take away a wanted include.
		for _, dir := range order {
			if rel, err := filepath.Rel(dir, ex); err == nil && !strings.HasPrefix(rel, "..") {
				groups[dir].exclude = append(groups[dir].exclude, filepath.ToSlash(rel))
			}
		}
	}

	out := make([]dangSourceGroup, 0, len(order))
	for _, dir := range order {
		out = append(out, *groups[dir])
	}
	return out
}

// relToModule rebases one of the SDK's workspace-relative patterns back to the
// module-relative form the generated program reads in, and reports whether it
// was an exclude.
//
// The SDK resolves what the user wrote against the workspace root, because that
// is the form its own generation-time reads need. `sources` reads through a
// workspace whose cwd is the module, so the rebase here is the inverse.
func (c *dangFuncCtx) relToModule(pattern, mod string) (string, bool) {
	p, negated := strings.CutPrefix(pattern, "!")
	rel, err := filepath.Rel(mod, p)
	if err != nil {
		// Only possible for a pattern the SDK never emits; keep it verbatim
		// rather than dropping a path the module needs.
		rel = p
	}
	return filepath.ToSlash(rel), negated
}

// packageManager is the manager the install step runs. The SDK detects it the
// way the engine's builtin runtime does and passes it in; the fallback is the
// runtime's own, the only one its base image is guaranteed to ship.
func (c *dangFuncCtx) packageManager() string {
	if c.opts.PackageManager != "" {
		return c.opts.PackageManager
	}
	switch c.opts.Runtime {
	case "bun":
		return "bun"
	case "deno":
		return "deno"
	default:
		return "npm"
	}
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
	files := []string{
		c.dispatchFile(),
		"clients/loader.gen.ts",
		"clients/dagger/index.ts",
		"clients/dagger/client.gen.ts",
		"clients/dagger/core.js",
	}
	switch c.opts.Runtime {
	case "deno":
		files = append(files, "deno.json")
	case "bun":
		files = append(files, "package.json")
	default:
		files = append(files, "package.json", c.tsConfigPath())
	}
	return files
}

// dangRequireGeneratedBody renders the body of requireGenerated: one existence
// check per required file, chained so the first missing one names itself.
//
// Rendered here rather than in the template because the file list is known at
// generation and an if/else-if chain is easier to read as flat output than as
// nested template actions.
func (c *dangFuncCtx) dangRequireGeneratedBody() string {
	var b strings.Builder
	// The module's own source, not the caller's workspace: a module loaded from
	// anywhere but the caller's checkout has generated files all the same, and
	// this check is about whether they were committed.
	b.WriteString("let dir = currentModule.source\n")
	b.WriteString("    ")

	for _, file := range c.dangRequiredFiles() {
		fmt.Fprintf(&b, "if (dir.exists(%s) == false) {\n", dangString(file))
		fmt.Fprintf(&b, "      raise %s\n", dangString(fmt.Sprintf(
			"module %q cannot run: the generated file %q is missing. "+
				"Run `dagger generate` and commit what it writes.",
			c.moduleName(), file)))
		b.WriteString("    } else ")
	}
	b.WriteString("{\n      null\n    }")

	return b.String()
}

// dangBaseChain renders the body of base(): the image, whatever the JS runtime
// needs before anything module-specific, and the package manager's download
// cache.
func (c *dangFuncCtx) dangBaseChain() string {
	var calls []string

	switch c.opts.Runtime {
	case "bun":
		calls = append(calls, fmt.Sprintf("from(%s)", dangString(c.baseImage(dangBunImageRef))))
	case "deno":
		calls = append(calls, fmt.Sprintf("from(%s)", dangString(c.baseImage(dangDenoImageRef))))
	default:
		calls = append(calls,
			fmt.Sprintf("from(%s)", dangString(c.baseImage(dangNodeImageRef))),
			`withExec(["apk", "add", "--no-cache", "ca-certificates"])`,
			`withEnvVariable("NODE_OPTIONS", "--use-openssl-ca")`,
		)
	}

	cachePath, cacheName := c.dangPackageCache()
	calls = append(calls, fmt.Sprintf("withMountedCache(%s, cacheVolume(%s))",
		dangString(cachePath), dangString(cacheName)))

	return dangChain("container", calls, "      ")
}

// baseImage is the image the recipe builds on: the module's configured
// `dagger.baseImage` when it has one, otherwise this SDK's pin for the runtime.
func (c *dangFuncCtx) baseImage(pinned string) string {
	if c.opts.BaseImage != "" {
		return c.opts.BaseImage
	}
	return pinned
}

// dangPackageCache is where the package manager keeps its downloads, and the
// cache volume backing it. Shared across every module on the engine that uses
// the same manager, the way the engine's builtin runtime shares its own.
func (c *dangFuncCtx) dangPackageCache() (path, volume string) {
	switch c.packageManager() {
	case "deno":
		// DENO_DIR in denoland/deno. Upstream mounts /root/.deno/cache
		// (runtime_deno.go:29), which is not where that image caches anything.
		return "/deno-dir", "dagger-typescript-deno"
	case "bun":
		return "/root/.bun/install/cache", "dagger-typescript-bun"
	case "yarn":
		return "/root/.cache/yarn", "dagger-typescript-yarn"
	case "pnpm":
		return "/root/.local/share/pnpm/store", "dagger-typescript-pnpm"
	default:
		return "/root/.npm", "dagger-typescript-npm"
	}
}

// dangManifestFiles lists the files the install reads, and nothing else: they
// are the whole cache key of the install layer, so a module's source can change
// without reinstalling anything. Optional ones are named too — the include
// filter simply drops whichever are absent.
func (c *dangFuncCtx) dangManifestFiles() []string {
	switch c.packageManager() {
	case "deno":
		return []string{"deno.json", "deno.lock"}
	case "bun":
		return []string{"package.json", "bun.lock", "bun.lockb", "bunfig.toml", ".npmrc"}
	case "yarn":
		return []string{"package.json", "yarn.lock", ".yarnrc.yml", ".npmrc"}
	case "pnpm":
		return []string{"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc"}
	default:
		return []string{"package.json", "package-lock.json", ".npmrc"}
	}
}

// dangInstallExecs renders the execs that install the module's dependencies,
// mirroring the engine's builtin runtime (runtime_node.go, runtime_bun.go,
// runtime_deno.go) — including its --prod / --omit=dev flags, so a module gets
// the same tree under an entrypoint as it does under a [runtime].
//
// A yarn pin the base image cannot run is the one place that mirror is not
// followed: the engine's own `yarn install --prod` aborts the same way, and
// only a module with a dependency of its own reaches it, because its yarn
// manager skips the install entirely for the default one.
func (c *dangFuncCtx) dangInstallExecs() []string {
	switch c.packageManager() {
	case "deno":
		return []string{`withExec(["deno", "install", "--node-modules-dir=auto"])`}
	case "bun":
		return []string{`withExec(["bun", "install", "--no-verify", "--omit=dev", "--omit=peer", "--omit=optional"])`}
	case "yarn":
		major, _, _ := strings.Cut(c.opts.PackageManagerVersion, ".")
		if major == "" || major == "1" {
			return []string{`withExec(["yarn", "install", "--prod"])`}
		}
		// The node image ships yarn 1 with corepack bundled but *not* enabled, so
		// yarn 1 refuses to run at all for a package.json pinning another major
		// rather than delegating to it. Enabling it is the setup exec yarn needs,
		// the way pnpm needs its global install — and the only one: yarn Berry is
		// not published to npm.
		return []string{
			`withExec(["corepack", "enable", "yarn"])`,
			// --activate so the pin still holds when it came from this SDK's
			// setting rather than from a packageManager field corepack can read:
			// the shim falls back to corepack's own default (yarn 1) otherwise.
			fmt.Sprintf(`withExec(["corepack", "prepare", %s, "--activate"])`,
				dangString(c.dangManagerSpec("yarn"))),
			// Berry takes no --prod, and its default PnP linker writes no
			// node_modules for the call to mount. The env var overrides a
			// .yarnrc.yml of the user's, which is the point: this recipe mounts
			// node_modules, so that is the one setting it cannot honour.
			//
			// shortcut: devDependencies come along, unlike every other manager
			// here — Berry's prod-only install is `yarn workspaces focus
			// --production`, a plugin command only yarn 4 bundles, and a yarn 2/3
			// pin has to keep installing. Upgrade when the pin's major is worth
			// branching on.
			`withEnvVariable("YARN_NODE_LINKER", "node-modules")`,
			`withExec(["yarn", "install"])`,
		}
	case "pnpm":
		return []string{
			fmt.Sprintf(`withExec(["npm", "install", "-g", %s])`, dangString(c.dangManagerSpec("pnpm"))),
			`withExec(["pnpm", "install", "--shamefully-hoist=true", "--prod"])`,
		}
	default:
		var calls []string
		// The image already ships the version the SDK resolves a lockfile to, so
		// only a pin that disagrees with it costs a global install.
		if v := c.opts.PackageManagerVersion; v != "" && v != dangNodeNpmVersion {
			calls = append(calls, fmt.Sprintf(`withExec(["npm", "install", "-g", %s])`,
				dangString("npm@"+v)))
		}
		return append(calls, `withExec(["npm", "install", "--omit=dev"])`)
	}
}

// dangManagerSpec is the npm spec that installs the package manager, pinned when
// the SDK resolved a version.
func (c *dangFuncCtx) dangManagerSpec(name string) string {
	if v := c.opts.PackageManagerVersion; v != "" {
		return name + "@" + v
	}
	return name
}

// dangDependenciesChain renders the body of the private dependencies() helper:
// the module's dependencies installed, as the node_modules a call mounts.
//
// It cannot run inside runtime(). The install has to happen before the workspace
// is mounted — every exec after that mount is keyed on the whole workspace, so a
// one-line source edit would reinstall — and anything written before it under
// the mount point is hidden by it.
func (c *dangFuncCtx) dangDependenciesChain() string {
	manifest := c.dangManifestFiles()
	includes := make([]string, 0, len(manifest)+1)
	for _, f := range manifest {
		includes = append(includes, dangString(f))
	}
	// The module's file: dependencies — clients/dagger and each client package —
	// have to be in the install context beside the manifest for the specifiers
	// to resolve. The whole clients/ tree comes along: it is generated content
	// that changes only on regeneration, so it keys the layer with the manifest
	// rather than the module's source, and a one-line src edit reinstalls
	// nothing.
	includes = append(includes, dangString("clients/**"))

	if len(c.opts.Include) == 0 {
		calls := []string{
			fmt.Sprintf("withWorkdir(%s)", dangString(dangInstallDir)),
			fmt.Sprintf("withDirectory(%s, currentModule.source, include: [%s])",
				dangString(dangInstallDir), strings.Join(includes, ", ")),
			// Seeded because a module that declares no dependency at all leaves
			// npm creating nothing, and the read below would then name a missing
			// path.
			fmt.Sprintf("withDirectory(%s, directory)", dangString(dangInstallDir+"/node_modules")),
		}
		calls = append(calls, c.dangInstallExecs()...)
		calls = append(calls, fmt.Sprintf("directory(%s)", dangString(dangInstallDir+"/node_modules")))
		return dangChain("base", calls, "      ")
	}

	// With resources the install needs the context: a file: dependency outside
	// the module directory has nothing to resolve to otherwise, and the symlink
	// npm leaves for it is relative — so the module has to sit at its own
	// subpath here, as it does under the runtime mount.
	//
	// The module's own tree is dropped from that context and its manifest read
	// back in, which keeps the install keyed on the manifests and the resources
	// rather than on the module's source.
	calls := []string{
		fmt.Sprintf(`withDirectory(%s, currentModule.contextDirectory.filter(exclude: [currentModule.sourceSubpath + "/**"]))`,
			dangString(dangInstallDir)),
		fmt.Sprintf("withDirectory(installDir, currentModule.source, include: [%s])",
			strings.Join(includes, ", ")),
		"withWorkdir(installDir)",
		`withDirectory(installDir + "/node_modules", directory)`,
	}
	calls = append(calls, c.dangInstallExecs()...)
	calls = append(calls, `directory(installDir + "/node_modules")`)

	return fmt.Sprintf("let installDir = %s + currentModule.sourceSubpath\n    %s",
		dangString(dangInstallDir+"/"), dangChain("base", calls, "      "))
}

// dangRuntimeChain renders the body of the private runtime() helper: everything
// up to but not including the per-call exec, so the whole build is shared across
// calls and across modules that share a prefix.
func (c *dangFuncCtx) dangRuntimeChain() string {
	var calls []string

	// tsx is a node-only loader, and installing it before anything
	// module-specific keys the layer on (image digest, tsx version) alone.
	switch c.opts.Runtime {
	case "bun", "deno":
	default:
		calls = append(calls,
			fmt.Sprintf(`withExec(["npm", "install", "-g", %s])`, dangString("tsx@"+dangTsxVersion)))
	}

	// The module context, not the caller's workspace: it holds the module
	// directory, the manifest and whatever `resources` lists, at their paths
	// from the context root — and nothing else of the repository. The engine
	// assembles it while it loads the module source, so it is also what the
	// module digest covers, which is what makes a call key follow a change to a
	// listed file.
	//
	// node_modules is excluded rather than mounted: it is a host build artifact
	// that can dwarf the source, and the one the call actually runs against is
	// mounted from dependencies just below.
	calls = append(calls,
		fmt.Sprintf(`withMountedDirectory(%s, currentModule.contextDirectory.filter(exclude: ["**/node_modules"]))`,
			dangString(dangContextDir)),
		fmt.Sprintf("withWorkdir(%s + currentModule.sourceSubpath)", dangString(dangContextDir+"/")),
		`withMountedDirectory("node_modules", dependencies)`,
	)

	return dangChain("base", calls, "      ")
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
