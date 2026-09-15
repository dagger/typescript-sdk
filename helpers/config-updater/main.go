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
	"path"
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
		// tsconfig INPUT OUTPUT [--packaged] CLIENTS_DIR [MODULE...] — one
		// @dagger.io/<module> path alias per generated module client. Without
		// --packaged the clients are flat files beside a sibling sdk/ library
		// (./<CLIENTS_DIR>/<module>.gen.ts, or ./<module>.gen.ts when CLIENTS_DIR
		// is empty); with it CLIENTS_DIR is the shared workspace client
		// directory of one package per module, the library among them.
		layout, rest, argErr := parseLayout(extra, "tsconfig")
		if argErr != nil {
			return argErr
		}
		updated, err = updateTSConfig(input, layout, rest)
	case "deno-config":
		// deno-config INPUT OUTPUT [--packaged] CLIENTS_DIR [MODULE...] — same
		// per-module aliases, as import-map entries.
		layout, rest, argErr := parseLayout(extra, "deno-config")
		if argErr != nil {
			return argErr
		}
		updated, err = updateDenoConfig(input, layout, rest)
	default:
		return fmt.Errorf("unknown subcommand %q (expected one of: package-json, tsconfig, deno-config)", subcommand)
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

// parseLayout reads the alias layout out of a subcommand's trailing arguments:
// an optional --packaged flag, the clients directory, then the module names.
func parseLayout(extra []string, subcommand string) (aliasLayout, []string, error) {
	packaged := false
	if len(extra) > 0 && extra[0] == "--packaged" {
		packaged = true
		extra = extra[1:]
	}
	if len(extra) < 1 {
		return aliasLayout{}, nil, fmt.Errorf("usage: config-updater %s INPUT_PATH OUTPUT_PATH [--packaged] CLIENTS_DIR [MODULE...]", subcommand)
	}
	return aliasLayout{clientsDir: extra[0], packaged: packaged}, extra[1:], nil
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

func updateTSConfig(tsConfig string, layout aliasLayout, modules []string) (string, error) {
	tsConfig, err := updateScopeAliases(tsConfig, "compilerOptions.paths", layout, modules, true)
	if err != nil {
		return "", err
	}

	tsConfig, err = sjson.Set(tsConfig, "compilerOptions.experimentalDecorators", true)
	if err != nil {
		return "", fmt.Errorf("set experimentalDecorators: %w", err)
	}

	return tsConfig, nil
}

// aliasLayout resolves where the SDK-owned aliases point for one scope layout.
// Embedded (the default): the library lives in the scope's own sdk/ and the
// client files sit flat in clientsDir. Packaged: clientsDir is the shared
// workspace client directory — one package directory per module, the library
// among them as dagger/ — and may sit outside the scope, so targets can climb
// (../../clients/...).
type aliasLayout struct {
	clientsDir string
	packaged   bool
}

func (l aliasLayout) libTarget(file string) string {
	if l.packaged {
		return relTarget(path.Join(l.clientsDir, "dagger", file))
	}
	return "./sdk/" + file
}

func (l aliasLayout) moduleTarget(module string) string {
	if l.packaged {
		return relTarget(path.Join(l.clientsDir, module, module+".gen.ts"))
	}
	if l.clientsDir == "" {
		return "./" + module + ".gen.ts"
	}
	return "./" + l.clientsDir + "/" + module + ".gen.ts"
}

// relTarget keeps a cleaned relative path spelled the way tsconfig and deno
// import maps expect: explicit ./ unless it already climbs.
func relTarget(p string) string {
	if strings.HasPrefix(p, "../") {
		return p
	}
	return "./" + p
}

// moduleAliasPrefix scopes the per-module specifiers a module's source imports
// its generated clients through: @dagger.io/<module> resolves to that module's
// generated client file. It is the @dagger.io npm scope, one segment per
// module — the same name a client would carry if published — and deliberately
// not under @dagger.io/dagger, which is the core library.
const moduleAliasScope = "@dagger.io/"

func moduleAlias(module string) string {
	return moduleAliasScope + module
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
// aliases at the vendored library, wherever the layout keeps it. asArray
// selects tsconfig's []string value shape over deno's plain string.
func syncLibAliases(jsonStr, keyPath string, layout aliasLayout, asArray bool) (string, error) {
	entries := []struct{ alias, target string }{
		{daggerLibPathAlias, layout.libTarget("index.ts")},
		{daggerTelemetryPathAlias, layout.libTarget("telemetry.ts")},
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

// updateScopeAliases syncs the SDK-owned aliases in a module scope's config —
// @dagger.io/dagger, its /telemetry sub-path, and one @dagger.io/<module> per
// generated client — leaving every other key alone.
func updateScopeAliases(jsonStr, keyPath string, layout aliasLayout, modules []string, asArray bool) (string, error) {
	jsonStr, err := syncLibAliases(jsonStr, keyPath, layout, asArray)
	if err != nil {
		return "", err
	}
	return syncModuleAliases(jsonStr, keyPath, layout, modules, asArray)
}

// syncModuleAliases makes the config's @dagger.io/<module> entries under
// keyPath match the given module list exactly: one alias per module, stale
// entries for modules that left the closure removed. The core @dagger.io/dagger
// aliases and any non-module key are untouched. asArray selects tsconfig's
// []string value shape over deno's plain string.
func syncModuleAliases(jsonStr, keyPath string, layout aliasLayout, modules []string, asArray bool) (string, error) {
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
		var value any = layout.moduleTarget(module)
		if asArray {
			value = []string{layout.moduleTarget(module)}
		}
		jsonStr, err = sjson.Set(jsonStr, keyPath+"."+gjson.Escape(moduleAlias(module)), value)
		if err != nil {
			return "", fmt.Errorf("set module alias for %s: %w", module, err)
		}
	}
	return jsonStr, nil
}

func updateDenoConfig(denoConfig string, layout aliasLayout, modules []string) (string, error) {
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

	denoConfig, err = updateScopeAliases(denoConfig, "imports", layout, modules, false)
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
