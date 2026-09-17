package typescriptgenerator

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
)

// TestGenerateDangEntrypoint pins the Dang program against the same typedef the
// dispatcher is rendered from, so the two halves of a module entrypoint stay
// derived from one scan. The fixture is the introspector's own output, and it
// covers what has to survive the retarget from TypeScript to Dang: a defaulted
// constructor argument, an exposed object field, an optional argument, a list
// argument, an enum with renamed members, an interface, and a void return.
func TestGenerateDangEntrypoint(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
			ModulePath:      ".",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)

	got := readOverlay(t, state, DefaultDangEntrypointFile)

	const goldenPath = "testdata/dang_entrypoint_smoke_want.dang"
	if *updateFixtures {
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	require.Equal(t, string(want), got)
}

// TestGenerateDangEntrypointDefaultConstructor covers a main class that
// declares no constructor, like the empty template's. The engine identifies an
// entrypoint module's main object by its constructor typedef alone — there is
// no name fallback and no engine-side default as for runtime modules — so
// without one here the module loads cleanly and still shows nowhere.
func TestGenerateDangEntrypointDefaultConstructor(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_no_ctor.json",
			Runtime:         "node",
			ModulePath:      ".",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Contains(t, got, strings.Join([]string{
		`      typeDef`,
		`        .withObject("Test", sourceMap: sourceMap("src/index.ts", 4, 14))`,
		`        .withConstructor(`,
		`          function("", typeDef.withObject("Test")),`,
		`        )`,
	}, "\n"))
	// Only the main object may carry one: the engine rejects a types() list
	// holding two constructors.
	require.Equal(t, 1, strings.Count(got, ".withConstructor("))
}

// TestGenerateDangEntrypointRuntimes pins the one part of the program that is not
// derived from the typedef: the container recipe call() bakes. Each runtime needs
// a different image, a different way to reach the dispatcher, and — for deno,
// which resolves @dagger.io/dagger through deno.json rather than node_modules —
// a different mount layout.
func TestGenerateDangEntrypointRuntimes(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		image   string
		exec    string
	}{
		{
			runtime: "node",
			image:   "node:24.13.1-alpine@sha256:",
			exec:    `["tsx", "--no-deprecation", "--tsconfig", "tsconfig.json", "__dagger.dispatch.ts", "engine-call"]`,
		},
		{
			runtime: "bun",
			image:   "oven/bun:1.3.0-alpine@sha256:",
			exec:    `["bun", "run", "__dagger.dispatch.ts", "engine-call"]`,
		},
		{
			runtime: "deno",
			image:   "denoland/deno:alpine-2.5.0@sha256:",
			exec:    `["deno", "run", "-q", "-A", "__dagger.dispatch.ts", "engine-call"]`,
		},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					Runtime:         tc.runtime,
					ModulePath:      ".",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			require.Contains(t, got, tc.image)
			require.Contains(t, got, tc.exec)

			// @dagger.io/dagger is installed as a package.json file: dependency
			// (clients/dagger), never folded into node_modules by the recipe.
			require.NotContains(t, got, `withDirectory("@dagger.io/dagger"`)

			// tsx is a node-only loader, installed because a generated entrypoint
			// has no engine image to mount it out of.
			if tc.runtime == "node" {
				require.Contains(t, got, `"npm", "install", "-g", "tsx@`)
			} else {
				require.NotContains(t, got, "tsx@")
			}
		})
	}
}

// TestGenerateDangEntrypointNestedModule covers a module that is not the
// workspace root. call() receives the *caller's* workspace, so the module's own
// path has to be baked at generation rather than read from workspace.cwd —
// otherwise a call from a subdirectory runs the dispatcher in the wrong place.
func TestGenerateDangEntrypointNestedModule(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
			ModulePath:      "./.dagger/modules/smoke",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Contains(t, got, `withWorkdir("/workspace/.dagger/modules/smoke")`)
	// The install reads the module's own manifest and clients/, not the
	// workspace root's.
	require.Contains(t, got, `workspace.directory("/.dagger/modules/smoke", include: [`)
}

// TestGenerateDangEntrypointInstallsDependencies pins the install step. Under a
// [runtime] the engine installs the module's dependencies before it runs
// anything; under an entrypoint nothing does that for us, so the recipe has to,
// with the manager the module actually uses.
func TestGenerateDangEntrypointInstallsDependencies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		runtime  string
		manager  string
		version  string
		exec     string
		cache    string
		manifest string
		setup    string
	}{
		{
			name: "npm", runtime: "node", manager: "npm", version: "11.8.0",
			exec:     `withExec(["npm", "install", "--omit=dev"])`,
			cache:    `withMountedCache("/root/.npm", cacheVolume("dagger-typescript-npm"))`,
			manifest: `"package.json", "package-lock.json", ".npmrc"`,
		},
		{
			name: "yarn", runtime: "node", manager: "yarn", version: "1.22.22",
			exec:     `withExec(["yarn", "install", "--prod"])`,
			cache:    `withMountedCache("/root/.cache/yarn", cacheVolume("dagger-typescript-yarn"))`,
			manifest: `"package.json", "yarn.lock", ".yarnrc.yml", ".npmrc"`,
		},
		{
			name: "pnpm", runtime: "node", manager: "pnpm", version: "8.15.4",
			exec:     `withExec(["pnpm", "install", "--shamefully-hoist=true", "--prod"])`,
			cache:    `withMountedCache("/root/.local/share/pnpm/store", cacheVolume("dagger-typescript-pnpm"))`,
			manifest: `"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc"`,
			setup:    `withExec(["npm", "install", "-g", "pnpm@8.15.4"])`,
		},
		{
			name: "bun", runtime: "bun", manager: "bun",
			exec:     `withExec(["bun", "install", "--no-verify", "--omit=dev", "--omit=peer", "--omit=optional"])`,
			cache:    `withMountedCache("/root/.bun/install/cache", cacheVolume("dagger-typescript-bun"))`,
			manifest: `"package.json", "bun.lock", "bun.lockb", "bunfig.toml", ".npmrc"`,
		},
		{
			name: "deno", runtime: "deno", manager: "deno",
			exec:     `withExec(["deno", "install", "--node-modules-dir=auto"])`,
			cache:    `withMountedCache("/deno-dir", cacheVolume("dagger-typescript-deno"))`,
			manifest: `"deno.json", "deno.lock"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath:       "testdata/typedef_smoke.json",
					Runtime:               tc.runtime,
					ModulePath:            ".",
					PackageManager:        tc.manager,
					PackageManagerVersion: tc.version,
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			require.Contains(t, got, tc.exec)
			require.Contains(t, got, tc.cache)
			// The clients/ tree rides along with the manifest: the module's
			// file: dependencies (clients/dagger and each client package) must be
			// in the install context for the specifiers to resolve.
			require.Contains(t, got, "include: ["+tc.manifest+`, "clients/**"]`)
			if tc.setup != "" {
				require.Contains(t, got, tc.setup)
			}

			// The install has to sit outside the workspace mount, which would
			// otherwise hide whatever it wrote, and the result reaches the call
			// as the module's node_modules.
			require.Contains(t, got, `withWorkdir("/deps")`)
			require.Contains(t, got, `directory("/deps/node_modules")`)
			require.Contains(t, got, `withMountedDirectory("node_modules", dependencies(workspace))`)
		})
	}
}

// TestGenerateDangEntrypointNpmPinMatchingImage covers the usual npm case: the
// SDK resolves a lockfile to the version the base image already ships, so the
// recipe should not spend a layer reinstalling it. Only a pin that disagrees
// with the image earns one.
func TestGenerateDangEntrypointNpmPinMatchingImage(t *testing.T) {
	render := func(t *testing.T, version string) string {
		t.Helper()
		gen := &TypeScriptGenerator{Config: generator.Config{
			DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
				TypedefJSONPath:       "testdata/typedef_smoke.json",
				Runtime:               "node",
				PackageManager:        "npm",
				PackageManagerVersion: version,
			},
		}}
		state, err := gen.GenerateDangEntrypoint(context.Background())
		require.NoError(t, err)
		return readOverlay(t, state, DefaultDangEntrypointFile)
	}

	require.NotContains(t, render(t, "11.8.0"), `"npm", "install", "-g", "npm@`)
	require.Contains(t, render(t, "10.9.0"), `withExec(["npm", "install", "-g", "npm@10.9.0"])`)
}

// TestGenerateDangEntrypointImplementsContract pins the shape the dang driver
// requires, all of which it rejects the module for getting wrong: exactly one
// type implementing ModuleEntrypoint, constructible with no arguments, and
// without re-declaring the interface — the driver writes that into the directory
// it loads, so a second declaration collides.
func TestGenerateDangEntrypointImplementsContract(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
		},
	}}

	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	got := readOverlay(t, state, DefaultDangEntrypointFile)

	require.Equal(t, 1, strings.Count(got, "implements ModuleEntrypoint"))
	require.Contains(t, got, "type Entrypoint implements ModuleEntrypoint {")
	require.NotContains(t, got, "interface ModuleEntrypoint")

	// No `let` field: a field, even a defaulted one, becomes a constructor
	// argument, and the driver requires a zero-argument constructor. A `let`
	// that takes arguments or has a body is a member, not a field.
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "let ") {
			continue
		}
		require.True(t, strings.Contains(trimmed, "(") || strings.HasSuffix(trimmed, "{"),
			"field %q would become a constructor argument", trimmed)
	}

	require.Contains(t, got, "pub types(workspace: Workspace!): [TypeDef!]! {")
	require.Contains(t, got, "pub call(")
}

func TestGenerateDangEntrypoint_RequiresTypedef(t *testing.T) {
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{},
	}}

	_, err := gen.GenerateDangEntrypoint(context.Background())
	require.ErrorContains(t, err, "TypedefJSONPath is required")
}

// TestGenerateDangEntrypointGuardsGeneratedFiles pins the guard call() runs
// before it builds anything.
//
// The engine's builtin TypeScript runtime has the same check, but under an
// entrypoint nothing runs that runtime — this program builds the container
// itself. Without the guard a gitignored sdk/ surfaces as a module-resolution
// error from inside a container, several layers from the cause.
func TestGenerateDangEntrypointGuardsGeneratedFiles(t *testing.T) {
	for _, tc := range []struct {
		runtime string
		wants   []string
		absent  string
	}{
		{runtime: "node", wants: []string{"package.json", "tsconfig.json"}, absent: "deno.json"},
		{runtime: "bun", wants: []string{"package.json"}, absent: "tsconfig.json"},
		{runtime: "deno", wants: []string{"deno.json"}, absent: "tsconfig.json"},
	} {
		t.Run(tc.runtime, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					ModuleName:      "smoke-mod",
					Runtime:         tc.runtime,
					ModulePath:      ".dagger/modules/smoke",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			// Called from call(), never from types(): a module whose generated
			// tree is missing still lists its functions, then fails on the first
			// real call with something that names the file and the fix.
			require.Contains(t, got, "let checked = requireGenerated(workspace)")
			require.Contains(t, got, `workspace.directory("/.dagger/modules/smoke")`)

			// Every file the recipe mounts or execs.
			for _, f := range append([]string{
				"__dagger.dispatch.ts", "clients/loader.gen.ts",
				"clients/dagger/index.ts", "clients/dagger/client.gen.ts", "clients/dagger/core.js",
			}, tc.wants...) {
				require.Contains(t, got, `dir.exists("`+f+`")`)
			}
			require.NotContains(t, got, `dir.exists("`+tc.absent+`")`)

			// The workspace's name for the module, not the typedef's pascalized
			// object name.
			require.Contains(t, got, `module \"smoke-mod\" cannot run`)
			require.Contains(t, got, "Run `dagger generate`")
		})
	}
}
