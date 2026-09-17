// config-updater idempotently merges Dagger-required keys into a TypeScript SDK
// module's config file (package.json, tsconfig.json, or deno.json), preserving
// any unrelated keys the user has set.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

const (
	daggerLibPathAlias       = "@dagger.io/dagger"
	daggerTelemetryPathAlias = "@dagger.io/dagger/telemetry"
)

var denoUnstableFlags = []string{
	"bare-node-builtins",
	"sloppy-imports",
	"node-globals",
	"byonm",
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: config-updater <subcommand> INPUT_PATH OUTPUT_PATH [args...]")
	}

	subcommand := args[0]
	inputPath := args[1]
	outputPath := args[2]
	extra := args[3:]

	input, err := readInput(inputPath)
	if err != nil {
		return err
	}

	var updated string
	switch subcommand {
	case "package-json":
		updated, err = updatePackageJSON(input)
	case "tsconfig":
		// tsconfig INPUT OUTPUT CORE_DIR CLIENTS_DIR [MODULE...] — point
		// @dagger.io/dagger at CORE_DIR (config-relative; empty = the module's
		// own ./sdk) and add one @dagger.io/<module> alias per generated client,
		// pointing at ./<CLIENTS_DIR>/<module>.gen.ts (or ./<module>.gen.ts when
		// CLIENTS_DIR is empty, for a flat client scope).
		if len(extra) < 2 {
			return fmt.Errorf("usage: config-updater tsconfig INPUT_PATH OUTPUT_PATH CORE_DIR CLIENTS_DIR [MODULE...]")
		}
		updated, err = updateTSConfig(input, extra[0], extra[1], extra[2:])
	case "deno-config":
		// deno-config INPUT OUTPUT CORE_DIR CLIENTS_DIR [MODULE...] — same
		// aliases, as import-map entries.
		if len(extra) < 2 {
			return fmt.Errorf("usage: config-updater deno-config INPUT_PATH OUTPUT_PATH CORE_DIR CLIENTS_DIR [MODULE...]")
		}
		updated, err = updateDenoConfig(input, extra[0], extra[1], extra[2:])
	case "shared-deps":
		// shared-deps INPUT OUTPUT CORE_REL [CLIENT_NAME=CLIENT_REL ...]
		//
		// Wire a package.json to the workspace's vendored shared core and to the
		// client packages this scope declares, all as file: dependencies in the
		// @dagger.io/* namespace the SDK owns. CORE_REL and each CLIENT_REL are
		// paths relative to the directory holding this package.json.
		if len(extra) < 1 {
			return fmt.Errorf("usage: config-updater shared-deps INPUT_PATH OUTPUT_PATH CORE_REL [NAME=REL ...]")
		}
		updated, err = updateSharedDeps(input, extra[0], extra[1:])
	default:
		return fmt.Errorf("unknown subcommand %q (expected one of: package-json, tsconfig, deno-config, shared-deps)", subcommand)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", subcommand, err)
	}

	// Indent every config we write, key order preserved. sjson edits in place,
	// which reads as "preserve the user's formatting" but only holds for the
	// parts it does not touch: keys it adds are appended compactly, and a file
	// created from scratch comes out as a single line. Committed config that
	// people read and edit is worth a whole-file reformat.
	//
	// Width is pretty's own default, not zero, which is what keeps a short array
	// on one line. Zero explodes `"@dagger.io/dagger": ["./sdk/index.ts"]` across
	// three lines, so the tsconfig.json we write stops matching the engine's byte
	// for byte — for two generators that are supposed to agree.
	out := pretty.PrettyOptions([]byte(updated), &pretty.Options{Indent: "  ", Width: 80})

	return os.WriteFile(outputPath, out, 0o644)
}

func readInput(path string) (string, error) {
	contents, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "{}", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	stripped := removeJSONComments(string(contents))
	if len(bytes.TrimSpace([]byte(stripped))) == 0 {
		return "{}", nil
	}
	return stripped, nil
}

func updatePackageJSON(packageJSON string) (string, error) {
	packageJSON, err := sjson.Set(packageJSON, "type", "module")
	if err != nil {
		return "", fmt.Errorf("set type=module: %w", err)
	}

	// Pin typescript unless the module already chose a version. The runtime
	// mounts its own prebuilt copy — and skips dependency installation for an
	// otherwise dependency-free module — only when the pin matches the engine's
	// default, so drifting from it silently turns every call into an install.
	packageJSON, err = pinTypeScript(packageJSON)
	if err != nil {
		return "", err
	}

	// Remove legacy in-tree @dagger.io/dagger deps so we transition cleanly to
	// the engine-managed bundle. Matches dagger/dagger UpdatePackageJSONForModule.
	for _, key := range []string{
		"dependencies." + gjson.Escape(daggerLibPathAlias),
		"devDependencies." + gjson.Escape(daggerLibPathAlias),
	} {
		packageJSON, err = sjson.Delete(packageJSON, key)
		if err != nil {
			return "", fmt.Errorf("delete %s: %w", key, err)
		}
	}

	return packageJSON, nil
}

// updateSharedDeps rewrites a package.json so the @dagger.io/* namespace it
// depends on matches exactly what this scope resolves: the vendored shared core
// as @dagger.io/dagger, and one @dagger.io/<name> per client this scope
// declares. Each is a file: dependency on a path relative to this package.
//
// This is the whole link under the "package manifest is the only link" design:
// the config the language's own resolver reads is what wires a consumer to a
// client, not a dagger.toml entry and not a mount. The SDK owns the whole
// @dagger.io/* namespace here, so a client that has left the scope has its dep
// pruned — anything under @dagger.io/* that this run does not write is removed,
// which is how a dropped client stops resolving.
//
// Non-@dagger.io dependencies, and every other key in the file, are the user's
// and are left untouched.
func updateSharedDeps(packageJSON, coreRel string, clientPairs []string) (string, error) {
	packageJSON, err := sjson.Set(packageJSON, "type", "module")
	if err != nil {
		return "", fmt.Errorf("set type=module: %w", err)
	}

	// Desired @dagger.io/* deps: core plus each declared client. Built first so
	// pruning below can drop anything not in it.
	desired := map[string]string{
		daggerLibPathAlias: "file:" + coreRel,
	}
	for _, pair := range clientPairs {
		name, rel, ok := strings.Cut(pair, "=")
		if !ok || name == "" || rel == "" {
			return "", fmt.Errorf("client spec %q must be NAME=REL", pair)
		}
		desired["@dagger.io/"+name] = "file:" + rel
	}

	// Prune only the SDK's own generated clients for this scope that this run no
	// longer writes — those are the "file:./<name>" deps beside the manifest. A
	// dep the user added by hand, pointing at another scope's client (a "file:../"
	// path) or a registry, is theirs to keep: installing a client generated
	// elsewhere for a self-call is a real use, and regeneration must not clobber
	// it. Core (@dagger.io/dagger, a "file:../" path) is always in `desired`, so
	// it is never touched here.
	if existing := gjson.Get(packageJSON, "dependencies"); existing.Exists() {
		for key, val := range existing.Map() {
			if !strings.HasPrefix(key, "@dagger.io/") {
				continue
			}
			if _, keep := desired[key]; keep {
				continue
			}
			if !strings.HasPrefix(val.String(), "file:./") {
				continue
			}
			packageJSON, err = sjson.Delete(packageJSON, "dependencies."+gjson.Escape(key))
			if err != nil {
				return "", fmt.Errorf("prune %s: %w", key, err)
			}
		}
	}

	// Write the desired set. Sorted so the output is deterministic regardless of
	// the order clients were passed in.
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		packageJSON, err = sjson.Set(packageJSON, "dependencies."+gjson.Escape(name), desired[name])
		if err != nil {
			return "", fmt.Errorf("set %s dependency: %w", name, err)
		}
	}

	return pinTypeScript(packageJSON)
}

// defaultTypeScriptVersion mirrors dagger/dagger tsdistconsts.DefaultTypeScriptVersion.
const defaultTypeScriptVersion = "5.9.3"

// pinTypeScript adds the default typescript pin unless the module already
// declares one, in either dependency section. devDependencies is the normal
// place to put a compiler, so writing dependencies.typescript without looking
// there leaves the module declaring two versions of the same package — npm
// resolves that to the runtime one, quietly overriding the compiler the user
// chose.
func pinTypeScript(packageJSON string) (string, error) {
	for _, section := range []string{"dependencies", "devDependencies"} {
		if gjson.Get(packageJSON, section+".typescript").Exists() {
			return packageJSON, nil
		}
	}

	packageJSON, err := sjson.Set(packageJSON, "dependencies.typescript", defaultTypeScriptVersion)
	if err != nil {
		return "", fmt.Errorf("set typescript dependency: %w", err)
	}
	return packageJSON, nil
}

// noLibAlias is the CORE_DIR sentinel that says "the SDK is not the resolver":
// write no @dagger.io/dagger path alias, and remove one a previous layout left.
// The shared-core node/bun layout uses it — the module resolves the library
// through package.json + an install, not a tsconfig path.
const noLibAlias = "-"

func updateTSConfig(tsConfig, coreDir, clientsDir string, modules []string) (string, error) {
	var err error
	if coreDir == noLibAlias {
		tsConfig, err = removeLibAliases(tsConfig, "compilerOptions.paths")
	} else {
		tsConfig, err = syncLibAliases(tsConfig, "compilerOptions.paths", libDir(coreDir), true)
	}
	if err != nil {
		return "", err
	}

	tsConfig, err = syncModuleAliases(tsConfig, "compilerOptions.paths", clientsDir, modules, true)
	if err != nil {
		return "", err
	}

	// A paths object emptied by the removals above is noise; drop it so the
	// module's tsconfig carries only what it needs.
	if gjson.Get(tsConfig, "compilerOptions.paths").Exists() && len(gjson.Get(tsConfig, "compilerOptions.paths").Map()) == 0 {
		tsConfig, err = sjson.Delete(tsConfig, "compilerOptions.paths")
		if err != nil {
			return "", err
		}
	}

	tsConfig, err = sjson.Set(tsConfig, "compilerOptions.experimentalDecorators", true)
	if err != nil {
		return "", fmt.Errorf("set experimentalDecorators: %w", err)
	}

	return tsConfig, nil
}

// removeLibAliases deletes the @dagger.io/dagger and /telemetry aliases under
// keyPath, for a scope where package.json rather than a path alias resolves the
// library.
func removeLibAliases(jsonStr, keyPath string) (string, error) {
	for _, alias := range []string{daggerLibPathAlias, daggerTelemetryPathAlias} {
		var err error
		jsonStr, err = sjson.Delete(jsonStr, keyPath+"."+gjson.Escape(alias))
		if err != nil {
			return "", fmt.Errorf("remove %s alias: %w", alias, err)
		}
	}
	return jsonStr, nil
}

// moduleAliasPrefix scopes the per-module specifiers a module's source imports
// its generated clients through: @dagger.io/<module> resolves to
// ./clients/<module>.gen.ts. It is the @dagger.io npm scope, one segment per
// module — the same name a client would carry if published — and deliberately
// not under @dagger.io/dagger, which is the core library.
const moduleAliasScope = "@dagger.io/"

func moduleAlias(module string) string {
	return moduleAliasScope + module
}

func moduleAliasTarget(clientsDir, module string) string {
	if clientsDir == "" {
		return "./" + module + ".gen.ts"
	}
	return "./" + clientsDir + "/" + module + ".gen.ts"
}

// isModuleAlias reports whether a config key is one of the per-module client
// aliases this writer owns: an @dagger.io scoped name with a single segment,
// excluding the core library @dagger.io/dagger (and thus its /telemetry
// sub-path). Those two are static runtime surface, never pruned.
func isModuleAlias(key string) bool {
	rest, ok := strings.CutPrefix(key, moduleAliasScope)
	if !ok || rest == "dagger" || strings.Contains(rest, "/") {
		return false
	}
	return true
}

// syncLibAliases points the @dagger.io/dagger and @dagger.io/dagger/telemetry
// aliases at the library under libDir — a path relative to the config, either
// the module's own "./sdk" or the workspace's shared core
// ("../../.dagger/core/typescript"). asArray selects tsconfig's []string value
// shape over deno's plain string.
func syncLibAliases(jsonStr, keyPath, libDir string, asArray bool) (string, error) {
	entries := []struct{ alias, target string }{
		{daggerLibPathAlias, libDir + "/index.ts"},
		{daggerTelemetryPathAlias, libDir + "/telemetry.ts"},
	}
	var err error
	for _, e := range entries {
		var value any = e.target
		if asArray {
			value = []string{e.target}
		}
		jsonStr, err = sjson.Set(jsonStr, keyPath+"."+gjson.Escape(e.alias), value)
		if err != nil {
			return "", fmt.Errorf("set %s alias: %w", e.alias, err)
		}
	}
	return jsonStr, nil
}

// libDir is the config-relative directory the @dagger.io/dagger aliases point
// at: the module's own embedded "./sdk" when empty, or the given relative path
// to the workspace's shared core.
func libDir(coreDir string) string {
	if coreDir == "" {
		return "./sdk"
	}
	return coreDir
}

// updateScopeAliases syncs the SDK-owned aliases in a module scope's config —
// @dagger.io/dagger, its /telemetry sub-path, and one @dagger.io/<module> per
// generated client — leaving every other key alone.
func updateScopeAliases(jsonStr, keyPath, libDir, clientsDir string, modules []string, asArray bool) (string, error) {
	jsonStr, err := syncLibAliases(jsonStr, keyPath, libDir, asArray)
	if err != nil {
		return "", err
	}
	return syncModuleAliases(jsonStr, keyPath, clientsDir, modules, asArray)
}

// syncModuleAliases makes the config's @dagger.io/<module> entries under
// keyPath match the given module list exactly: one alias per module, stale
// entries for modules that left the closure removed. The core @dagger.io/dagger
// aliases and any non-module key are untouched. asArray selects tsconfig's
// []string value shape over deno's plain string.
func syncModuleAliases(jsonStr, keyPath, clientsDir string, modules []string, asArray bool) (string, error) {
	keep := map[string]bool{}
	for _, module := range modules {
		keep[moduleAlias(module)] = true
	}

	var stale []string
	gjson.Get(jsonStr, keyPath).ForEach(func(key, _ gjson.Result) bool {
		if k := key.String(); isModuleAlias(k) && !keep[k] {
			stale = append(stale, k)
		}
		return true
	})

	var err error
	for _, k := range stale {
		jsonStr, err = sjson.Delete(jsonStr, keyPath+"."+gjson.Escape(k))
		if err != nil {
			return "", fmt.Errorf("remove stale module alias %s: %w", k, err)
		}
	}
	for _, module := range modules {
		var value any = moduleAliasTarget(clientsDir, module)
		if asArray {
			value = []string{moduleAliasTarget(clientsDir, module)}
		}
		jsonStr, err = sjson.Set(jsonStr, keyPath+"."+gjson.Escape(moduleAlias(module)), value)
		if err != nil {
			return "", fmt.Errorf("set module alias for %s: %w", module, err)
		}
	}
	return jsonStr, nil
}

func updateDenoConfig(denoConfig, coreDir, clientsDir string, modules []string) (string, error) {
	// Deno resolves dependencies through this map rather than node_modules, so
	// the compiler the module's own code needs has to be declared here.
	denoConfig, err := setIfNotExists(denoConfig, "imports.typescript", "npm:typescript@"+defaultTypeScriptVersion)
	if err != nil {
		return "", fmt.Errorf("set typescript import: %w", err)
	}

	denoConfig, err = sjson.Set(denoConfig, "nodeModulesDir", "auto")
	if err != nil {
		return "", fmt.Errorf("set nodeModulesDir: %w", err)
	}

	for _, flag := range denoUnstableFlags {
		denoConfig, err = appendIfNotExists(denoConfig, "unstable", flag)
		if err != nil {
			return "", fmt.Errorf("append unstable %s: %w", flag, err)
		}
	}

	denoConfig, err = sjson.Set(denoConfig, "compilerOptions.experimentalDecorators", true)
	if err != nil {
		return "", fmt.Errorf("set experimentalDecorators: %w", err)
	}

	denoConfig, err = updateScopeAliases(denoConfig, "imports", libDir(coreDir), clientsDir, modules, false)
	if err != nil {
		return "", err
	}

	return denoConfig, nil
}

// setIfNotExists sets path to value only when path is absent, preserving any
// user-provided value. Mirrors tsutils.setIfNotExists.
func setIfNotExists(jsonStr, path, value string) (string, error) {
	return setValueIfNotExists(jsonStr, path, value)
}

// setValueIfNotExists is setIfNotExists for non-string JSON values (bool, etc.).
func setValueIfNotExists(jsonStr, path string, value any) (string, error) {
	if gjson.Get(jsonStr, path).Exists() {
		return jsonStr, nil
	}
	return sjson.Set(jsonStr, path, value)
}

func appendIfNotExists(jsonStr, path, value string) (string, error) {
	for _, v := range gjson.Get(jsonStr, path).Array() {
		if v.String() == value {
			return jsonStr, nil
		}
	}
	return sjson.Set(jsonStr, path+".-1", value)
}

// removeJSONComments strips // line comments so sjson can parse user configs
// that include JSONC-style comments (common in tsconfig.json).
func removeJSONComments(input string) string {
	var out bytes.Buffer
	inString := false
	escaped := false
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		c := runes[i]

		if c == '"' && !escaped {
			inString = !inString
		}

		if !inString && c == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			out.WriteRune('\n')
			continue
		}

		out.WriteRune(c)
		escaped = (c == '\\' && !escaped)
	}

	return out.String()
}
