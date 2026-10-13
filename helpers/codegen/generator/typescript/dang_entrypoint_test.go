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

// TestGenerateDangEntrypointDefaultConstructor covers the two halves of the
// constructor rule, from the fixture that holds both: a main class declaring no
// constructor, like the empty template's, beside a secondary class that does.
//
// The main object gets one whether or not it declared it. The engine identifies
// an entrypoint module's main object by its constructor typedef alone — there is
// no name fallback and no engine-side default as for runtime modules — so
// without one here the module loads cleanly and still shows nowhere.
//
// The secondary object gets none, however it was written: a second constructor
// in types() makes the engine reject the whole module with "multiple object
// constructors are not supported" (#89). Nothing is lost — a secondary class is
// constructed by the module's own code, never by the engine.
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

	// The secondary class declares `constructor(text: string)`; its entry ends
	// at withObject, so its argument never reaches types() either.
	require.Contains(t, got, strings.Join([]string{
		`      typeDef`,
		`        .withObject("Helper", sourceMap: sourceMap("src/index.ts", 7, 14)),`,
	}, "\n"))
	require.NotContains(t, got, `typeDef.withObject("Helper"))`)

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

// TestGenerateDangEntrypointBaseImage covers the module's `dagger.baseImage`:
// under a [runtime] the engine reads it and builds on it, so an entrypoint
// recipe that kept this SDK's pin would silently run the module on a different
// image than the one its config names (#91).
func TestGenerateDangEntrypointBaseImage(t *testing.T) {
	for _, runtime := range []string{"node", "bun", "deno"} {
		t.Run(runtime, func(t *testing.T) {
			gen := &TypeScriptGenerator{Config: generator.Config{
				DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
					TypedefJSONPath: "testdata/typedef_smoke.json",
					Runtime:         runtime,
					ModulePath:      ".",
					BaseImage:       "node:22-alpine",
				},
			}}

			state, err := gen.GenerateDangEntrypoint(context.Background())
			require.NoError(t, err)
			got := readOverlay(t, state, DefaultDangEntrypointFile)

			require.Contains(t, got, `from("node:22-alpine")`)
			require.NotContains(t, got, "@sha256:")
		})
	}
}

// renderDangEntrypoint is the recipe for one module path and include list.
func renderDangEntrypoint(t *testing.T, modulePath string, include ...string) string {
	t.Helper()
	gen := &TypeScriptGenerator{Config: generator.Config{
		DangEntrypointConfig: &generator.DangEntrypointGeneratorConfig{
			TypedefJSONPath: "testdata/typedef_smoke.json",
			Runtime:         "node",
			ModulePath:      modulePath,
			Include:         include,
		},
	}}
	state, err := gen.GenerateDangEntrypoint(context.Background())
	require.NoError(t, err)
	return readOverlay(t, state, DefaultDangEntrypointFile)
}

// TestGenerateDangEntrypointMountsTheModuleContext pins what a call can see.
//
// The recipe used to mount `workspace.directory("/")` — the caller's whole
// workspace — which put every unrelated file in the repository inside the
// module's container (.git, build output, a root .env) and keyed the container
// on all of them. It now mounts the module context the engine assembled: the
// module directory, the manifest and whatever `resources` lists, and nothing
// else. The caller's workspace is not read at all, which is also what lets a
// module loaded from anywhere but the caller's checkout run.
func TestGenerateDangEntrypointMountsTheModuleContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mod     string
		include []string
	}{
		{name: "root", mod: "."},
		{name: "nested", mod: "apps/web"},
		{name: "with resources", mod: "apps/web", include: []string{"apps/shared"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDangEntrypoint(t, tc.mod, tc.include...)

			// One mount, whatever the module's path and whatever it declares:
			// the engine decides what the context holds, and sourceSubpath
			// locates the module in it — which, unlike a baked path, is right
			// for a module whose context root is not the workspace root.
			require.Contains(t, got,
				`withMountedDirectory("/workspace", currentModule.contextDirectory.filter(exclude: ["**/node_modules"]))`)
			require.Contains(t, got, `withWorkdir("/workspace/" + currentModule.sourceSubpath)`)

			// Nothing reads the workspace any more, in any of the three
			// helpers the recipe is built from.
			require.NotContains(t, got, "workspace.directory(")
			require.NotContains(t, got, "workspace.cwd")
			require.Contains(t, got, "let dir = currentModule.source")
		})
	}
}

// TestGenerateDangEntrypointResources pins the member the engine reads while it
// loads the module source. The engine adds its patterns to the module context
// and to the module digest, which is what makes a call key follow a change to a
// listed file — the whole reason the list is declared rather than read.
func TestGenerateDangEntrypointResources(t *testing.T) {
	// The SDK resolves what the user wrote against the workspace root; the
	// engine resolves `resources` against the module directory. So the member
	// carries the module-relative form back.
	got := renderDangEntrypoint(t, "apps/web",
		"apps/shared", "!apps/shared/testdata", "apps/shared/*.ts", "apps/version.txt")
	require.Contains(t, got, strings.Join([]string{
		`  pub resources: [String!]! {`,
		`    ["../shared", "!../shared/testdata", "../shared/*.ts", "../version.txt"]`,
		`  }`,
	}, "\n"))

	// A deeper module, to catch a rebase that only works one level up.
	require.Contains(t,
		renderDangEntrypoint(t, ".dagger/modules/web", ".dagger/shared"),
		`["../../shared"]`)

	// Declared nothing, so the member is absent entirely and the engine loads
	// the module as it did before it existed.
	for _, mod := range []string{".", "apps/web"} {
		require.NotContains(t, renderDangEntrypoint(t, mod), "resources")
	}
}

// TestGenerateDangEntrypointResourcesInstall pins the install context. A file:
// dependency pointing outside the module directory has nothing to resolve to
// without the context, and the symlink npm leaves for it is relative — so the
// module has to sit at its own subpath here, as it does under the runtime mount.
func TestGenerateDangEntrypointResourcesInstall(t *testing.T) {
	got := renderDangEntrypoint(t, "apps/web", "apps/shared")

	require.Contains(t, got, `let installDir = "/deps/" + currentModule.sourceSubpath`)
	require.Contains(t, got,
		`withDirectory("/deps", currentModule.contextDirectory.filter(exclude: [currentModule.sourceSubpath + "/**"]))`)
	// The module's own tree is dropped from that context and its manifest read
	// back in, so the install stays keyed on the manifests and the resources
	// rather than on the module's source.
	require.Contains(t, got,
		`withDirectory(installDir, currentModule.source, include: ["package.json", "package-lock.json", ".npmrc", "clients/**"])`)
	require.Contains(t, got, `directory(installDir + "/node_modules")`)

	// Without resources the install reads the module directory alone, and needs
	// no subpath at all.
	plain := renderDangEntrypoint(t, "apps/web")
	require.Contains(t, plain, `withWorkdir("/deps")`)
	require.Contains(t, plain,
		`withDirectory("/deps", currentModule.source, include: ["package.json", "package-lock.json", ".npmrc", "clients/**"])`)
	require.NotContains(t, plain, "installDir")
	require.NotContains(t, plain, `withDirectory("/deps", currentModule.contextDirectory`)
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
			require.Contains(t, got, `withMountedDirectory("node_modules", dependencies)`)
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
	//
	// Only the type's own members qualify, which is what the two-space indent
	// selects — a `let` deeper than that is a local binding in some member's
	// body and binds nothing on the type.
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "  let ") {
			continue
		}
		trimmed := strings.TrimSpace(line)
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
			require.Contains(t, got, "let checked = requireGenerated")
			require.Contains(t, got, "let dir = currentModule.source")

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

// TestGenerateDangEntrypointRaisesDispatchErrors pins the entrypoint's half of
// the reply protocol: call() decodes what the dispatcher wrote and raises a
// carried error's message, so a function that throws fails with what it threw
// rather than with the exec's "exit code: 1".
func TestGenerateDangEntrypointRaisesDispatchErrors(t *testing.T) {
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

	require.Contains(t, got, ") :: DispatchReply!")
	require.Contains(t, got, "let failure = reply.error\n    if (failure != null) {\n      raise failure.message\n    }")
	require.NotContains(t, got, ".stdout :: JSON!")

	// The reply types are declared beside the entrypoint, not inside it: a
	// field on Entrypoint would become a constructor argument, which the dang
	// driver rejects (see TestGenerateDangEntrypointImplementsContract).
	require.Contains(t, got, "\n}\n\n\"\"\"\nWhat the dispatcher writes to stdout")
	require.Contains(t, got, "type DispatchReply {\n  result: JSON\n  error: DispatchError\n}")
	require.Contains(t, got, "type DispatchError {\n  message: String!\n}")
}
