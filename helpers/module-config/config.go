package main

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// getPackageManager reads the top-level "packageManager" field from a
// package.json. Returns "" when the key is absent.
func getPackageManager(input string) string {
	return gjson.Get(input, "packageManager").String()
}

// setPackageManager writes the top-level "packageManager" field.
func setPackageManager(input, value string) (string, error) {
	return sjson.Set(input, "packageManager", value)
}

// unsetPackageManager removes the top-level "packageManager" field.
func unsetPackageManager(input string) (string, error) {
	return sjson.Delete(input, "packageManager")
}

// getBaseImage reads "dagger.baseImage" from either a package.json or a
// deno.json (the engine accepts the same key in both). Returns "" when absent.
func getBaseImage(input string) string {
	return gjson.Get(input, "dagger.baseImage").String()
}

// setBaseImage writes "dagger.baseImage", creating the nested "dagger" object
// when necessary.
func setBaseImage(input, value string) (string, error) {
	return sjson.Set(input, "dagger.baseImage", value)
}

// globChars make an include entry a pattern rather than a plain path.
const globChars = "*?["

// resolveInclude reads "dagger.include" and resolves every entry against the
// module directory, returning workspace-relative patterns in declaration order.
// A leading "!" marks an exclude and survives on the resolved pattern.
//
// What a user writes is relative to the module directory; the workspace root is
// the boundary. Three refusals, because each would otherwise silently do
// nothing: an absolute path, a path that climbs past the workspace root (the
// entrypoint reads these files from the Workspace and can reach nothing above
// it), and a path that stays inside the module directory (already part of the
// module source).
func resolveInclude(input, modulePath string) ([]string, error) {
	field := gjson.Get(input, "dagger.include")
	if !field.Exists() {
		return nil, nil
	}
	if !field.IsArray() {
		return nil, errors.New("dagger.include must be an array of paths")
	}
	entries := field.Array()
	if len(entries) == 0 {
		return nil, nil
	}

	mod := strings.Trim(path.Clean("/"+modulePath), "/")
	if mod == "" {
		return nil, errors.New("dagger.include is not supported for a module at the workspace root: it already builds with every file in the workspace")
	}
	for _, part := range strings.Split(mod, "/") {
		// The introspector records a scanned file's path by finding the first
		// "src" component of its absolute path (ast.ts getNodeLocation). An
		// include makes the module scan from inside a workspace-rooted tree, so
		// a "src" above the module directory would be found first and every
		// source map and dispatcher import would name the wrong file.
		if part == "src" {
			return nil, fmt.Errorf("dagger.include is not supported for a module under a \"src\" directory (%q): move the module, or drop the field", modulePath)
		}
	}

	var out []string
	for _, entry := range entries {
		if entry.Type != gjson.String {
			return nil, fmt.Errorf("dagger.include: %s is not a path", entry.Raw)
		}
		resolved, err := resolveIncludeEntry(entry.String(), mod)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved...)
	}
	return out, nil
}

// resolveIncludeEntry resolves one entry to the patterns that select it from the
// workspace root.
func resolveIncludeEntry(entry, mod string) ([]string, error) {
	rel, negated := strings.CutPrefix(entry, "!")
	switch {
	case rel == "":
		return nil, fmt.Errorf("dagger.include: %q names no path", entry)
	case strings.HasPrefix(rel, "/"):
		return nil, fmt.Errorf("dagger.include: %q must be relative to the module directory", entry)
	}

	resolved := path.Join(mod, rel)
	switch {
	case resolved == ".." || strings.HasPrefix(resolved, "../"):
		return nil, fmt.Errorf("dagger.include: %q leaves the workspace root", entry)
	case resolved == mod || strings.HasPrefix(resolved, mod+"/"):
		return nil, fmt.Errorf("dagger.include: %q is inside the module directory, which the module already builds with", entry)
	}

	patterns := []string{resolved}
	// A plain path may name a file or a directory and the engine's filter
	// matches it literally, so name the entry and everything under it.
	if !strings.ContainsAny(rel, globChars) {
		patterns = append(patterns, resolved+"/**")
	}
	if negated {
		for i, p := range patterns {
			patterns[i] = "!" + p
		}
	}
	return patterns, nil
}

// unsetBaseImage removes "dagger.baseImage" and prunes the "dagger" object
// when it would otherwise be left empty, so the file does not carry an empty
// table after the round-trip.
func unsetBaseImage(input string) (string, error) {
	out, err := sjson.Delete(input, "dagger.baseImage")
	if err != nil {
		return "", err
	}
	dagger := gjson.Get(out, "dagger")
	if dagger.IsObject() && len(dagger.Map()) == 0 {
		return sjson.Delete(out, "dagger")
	}
	return out, nil
}
