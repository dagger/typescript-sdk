// config-updator idempotently merges Dagger-required keys into a TypeScript SDK
// module's config file (package.json, tsconfig.json, or deno.json), preserving
// any unrelated keys the user has set.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

const (
	daggerLibPathAlias       = "@dagger.io/dagger"
	daggerTelemetryPathAlias = "@dagger.io/dagger/telemetry"

	daggerLibPath          = "./sdk/index.ts"
	daggerTelemetryLibPath = "./sdk/telemetry.ts"
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
		return fmt.Errorf("usage: config-updator <subcommand> INPUT_PATH OUTPUT_PATH [args...]")
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
		updated, err = updateTSConfig(input)
	case "deno-config":
		updated, err = updateDenoConfig(input)
	case "client-package-json":
		// client-package-json INPUT OUTPUT ENGINE_VERSION MODULE_NAME
		if len(extra) != 2 {
			return fmt.Errorf("usage: config-updator client-package-json INPUT_PATH OUTPUT_PATH ENGINE_VERSION MODULE_NAME")
		}
		updated, err = updateClientPackageJSON(input, extra[0], extra[1])
	case "client-tsconfig":
		updated, err = updateClientTSConfig(input)
	case "client-deno-config":
		// client-deno-config INPUT OUTPUT ENGINE_VERSION
		if len(extra) != 1 {
			return fmt.Errorf("usage: config-updator client-deno-config INPUT_PATH OUTPUT_PATH ENGINE_VERSION")
		}
		updated, err = updateClientDenoConfig(input, extra[0])
	default:
		return fmt.Errorf("unknown subcommand %q (expected one of: package-json, tsconfig, deno-config, client-package-json)", subcommand)
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

// updateClientPackageJSON turns the client output dir's package.json into a
// self-contained scoped package: it pins @dagger.io/dagger to the module's
// engine version, pins typescript, and names the package when unnamed. Existing
// user config is preserved — in particular a @dagger.io/dagger set to a local
// ref (e.g. "./sdk", "file:../dagger") is left untouched, so a dev/unreleased
// engine can point at a local bundle. A missing/empty file starts from "{}".
func updateClientPackageJSON(packageJSON, engineVersion, moduleName string) (string, error) {
	packageJSON, err := sjson.Set(packageJSON, "type", "module")
	if err != nil {
		return "", fmt.Errorf("set type=module: %w", err)
	}

	// Name the package only when the user hasn't. Scoped, derived from the bound
	// module: @dagger.io/<sanitized module name>-client.
	if !gjson.Get(packageJSON, "name").Exists() {
		packageJSON, err = sjson.Set(packageJSON, "name", scopedClientName(moduleName))
		if err != nil {
			return "", fmt.Errorf("set name: %w", err)
		}
	}

	// The SDK owns the @dagger.io/dagger version pin, so it tracks the engine on
	// regeneration — but only step aside for a *local* ref the user has set (a
	// vendored bundle). Never clobber "./sdk"/"file:"/… with a version.
	daggerDepPath := "dependencies." + gjson.Escape(daggerLibPathAlias)
	if !isLocalDaggerRef(gjson.Get(packageJSON, daggerDepPath).String()) {
		packageJSON, err = sjson.Set(packageJSON, daggerDepPath, npmVersion(engineVersion))
		if err != nil {
			return "", fmt.Errorf("set @dagger.io/dagger dependency: %w", err)
		}
	}

	packageJSON, err = pinTypeScript(packageJSON)
	if err != nil {
		return "", err
	}

	return packageJSON, nil
}

// isLocalDaggerRef reports whether a package.json dependency value points at a
// local/non-registry source the SDK must not overwrite with a version pin
// (local paths, file:/link:/workspace: specifiers, git or URL refs).
func isLocalDaggerRef(value string) bool {
	for _, prefix := range []string{".", "/", "file:", "link:", "workspace:", "git+", "git:", "http:", "https:"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// npmVersion converts a dagger engine version to an npm-compatible one: strip a
// single leading "v" (v0.18.0 -> 0.18.0). A dev/pre-release suffix is kept as-is
// (design §7): the package may be unpublishable but stays installable/executable
// against that engine.
func npmVersion(engineVersion string) string {
	return strings.TrimPrefix(engineVersion, "v")
}

// scopedClientName derives @dagger.io/<sanitized>-client from the bound module
// name: lower-cased, every run of non-alphanumerics collapsed to a single "-",
// and leading/trailing "-" trimmed.
func scopedClientName(moduleName string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(moduleName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		// No usable module name — fall back to a bare scoped name.
		return "@dagger.io/client"
	}
	return "@dagger.io/" + sanitized + "-client"
}

// updateClientTSConfig fills a sane default tsconfig for a generated client,
// preserving any keys the user already set. Unlike the module variant it adds
// **no** `@dagger.io/dagger` -> `./sdk` path override: in the always-Remote model
// the bare specifier resolves from node_modules via the package.json dependency.
func updateClientTSConfig(tsConfig string) (string, error) {
	defaults := []struct {
		path  string
		value any
	}{
		{"compilerOptions.target", "ES2022"},
		{"compilerOptions.moduleResolution", "Node"},
		{"compilerOptions.experimentalDecorators", true},
		{"compilerOptions.strict", true},
		{"compilerOptions.skipLibCheck", true},
	}
	for _, d := range defaults {
		v, err := setValueIfNotExists(tsConfig, d.path, d.value)
		if err != nil {
			return "", fmt.Errorf("set %s: %w", d.path, err)
		}
		tsConfig = v
	}
	return tsConfig, nil
}

// updateClientDenoConfig configures deno.json for a generated client: the common
// Dagger deno setup plus — for the always-Remote model — the SDK-owned
// `@dagger.io/dagger` imports pinned to the engine version as `npm:` specifiers.
// (Upstream's remote path assumed the user declared these; a generated client
// dir has none, so the SDK writes them.)
func updateClientDenoConfig(denoConfig, engineVersion string) (string, error) {
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

	npmDagger := "npm:" + daggerLibPathAlias + "@" + npmVersion(engineVersion)
	denoConfig, err = sjson.Set(denoConfig, "imports."+gjson.Escape(daggerLibPathAlias), npmDagger)
	if err != nil {
		return "", fmt.Errorf("set @dagger.io/dagger import: %w", err)
	}
	denoConfig, err = sjson.Set(denoConfig, "imports."+gjson.Escape(daggerTelemetryPathAlias), npmDagger+"/telemetry")
	if err != nil {
		return "", fmt.Errorf("set @dagger.io/dagger/telemetry import: %w", err)
	}

	return denoConfig, nil
}

func updateTSConfig(tsConfig string) (string, error) {
	tsConfig, err := sjson.Set(tsConfig,
		"compilerOptions.paths."+gjson.Escape(daggerLibPathAlias),
		[]string{daggerLibPath},
	)
	if err != nil {
		return "", fmt.Errorf("set dagger path alias: %w", err)
	}

	tsConfig, err = sjson.Set(tsConfig,
		"compilerOptions.paths."+gjson.Escape(daggerTelemetryPathAlias),
		[]string{daggerTelemetryLibPath},
	)
	if err != nil {
		return "", fmt.Errorf("set dagger telemetry path alias: %w", err)
	}

	tsConfig, err = sjson.Set(tsConfig, "compilerOptions.experimentalDecorators", true)
	if err != nil {
		return "", fmt.Errorf("set experimentalDecorators: %w", err)
	}

	return tsConfig, nil
}

func updateDenoConfig(denoConfig string) (string, error) {
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

	denoConfig, err = sjson.Set(denoConfig,
		"imports."+gjson.Escape(daggerLibPathAlias),
		daggerLibPath,
	)
	if err != nil {
		return "", fmt.Errorf("set dagger import: %w", err)
	}

	denoConfig, err = sjson.Set(denoConfig,
		"imports."+gjson.Escape(daggerTelemetryPathAlias),
		daggerTelemetryLibPath,
	)
	if err != nil {
		return "", fmt.Errorf("set dagger telemetry import: %w", err)
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
