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

	"github.com/tidwall/gjson"
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
	if len(args) != 3 {
		return fmt.Errorf("usage: config-updator <package-json|tsconfig|deno-config> INPUT_PATH OUTPUT_PATH")
	}

	subcommand := args[0]
	inputPath := args[1]
	outputPath := args[2]

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
	default:
		return fmt.Errorf("unknown subcommand %q (expected one of: package-json, tsconfig, deno-config)", subcommand)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", subcommand, err)
	}

	return os.WriteFile(outputPath, []byte(updated), 0o644)
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
	denoConfig, err := sjson.Set(denoConfig, "nodeModulesDir", "auto")
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
