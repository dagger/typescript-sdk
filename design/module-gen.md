# Move TypeScript module codegen into the TypeScript SDK module

Status: proposed. Companion to [`client-gen.md`](./client-gen.md), which did the
same for **clients**; this one does **modules**.

## 1. Goal

Today a TypeScript **module**'s generated files (`sdk/`, `__dagger.entrypoint.ts`,
`package.json`/`tsconfig.json`/`deno.json`, lockfile) are produced by the engine:
`dagger generate` → this SDK's `generateAllModule` → `polyfill.moduleSource.generate`
→ engine `runSDKCodegen` → the **built-in** TypeScript runtime module
(`sdk/typescript/runtime` in `dagger/dagger`, a Go Dagger module) → `cmd/codegen`
+ `ts-introspector` in nested-engine containers.

We want this `.dang` SDK module to generate those files itself, the way it now
generates clients: plain data in (schema JSON, module metadata), plain container
execs, changeset out — and **no round trip through the engine's TS runtime**.

Same three motivations as the client move:

1. **Ownership.** The SDK repo owns what the SDK produces. Today a codegen fix
   means an engine release.
2. **Simplification.** The upstream path carries three SDK-lib origins, a
   Go+TypeScript shared binary, module/client coexistence, per-runtime
   duplication (node/bun/deno) and a runtime-vs-generate double life. Our
   contract is much narrower, so most of it is dead on arrival.
3. **Testability.** Codegen becomes `go test` + `dagger check` in this repo
   instead of an engine integration test.

**Non-goal: the module *runtime*.** Running a module (`dagger call`) stays with
the engine's built-in TypeScript runtime. That is a deliberate split — and it is
also the hardest constraint on this design (§2.2).

The one thing this move cannot do without is the bundled `@dagger.io/dagger`
library, which today only exists inside the engine image. **Decided: we build it
here**, with a packager module that commits the artifact into this repo (§4).

## 2. How module codegen works upstream today

### 2.1 Two consumers, one generator — and only one of them is left

`sdk/typescript/runtime` exposes two entry points that both run codegen:

- **`Codegen(modSource, introspectionJSON) → GeneratedCode`** — generate time
  (`dagger generate` / `dagger develop`). Returns the overlay written to the
  user's tree.
- **`ModuleRuntime(modSource, introspectionJSON?) → Container`** — call time.
  Historically it regenerated everything *inside* the container on every
  `dagger call`.

That second life is already gone for CLI 1.0 modules. `useRuntimeCodegen`
(`core/sdk/utils.go:23`) is decided by config format alone:

```go
// dagger.json → always regenerate at runtime (legacy)
// dagger-module.toml → never; build from committed files
return src.Self().ConfigFilename != modules.Filename
```

For a `dagger-module.toml` module the engine **omits** `introspectionJson`
(`core/sdk/module_runtime.go:50`), and the runtime takes
`setupContainerWithoutCodegen` (`runtime_node.go:149`): mount the tree as-is,
install deps, run the committed entrypoint.

**Consequence — this is the load-bearing fact of the whole design:** for
workspace modules, whatever `dagger generate` writes to disk *is* the artifact
the runtime executes. There is no second chance to fix it up at call time. So
moving `Codegen` here is a clean, complete move; but the output must satisfy an
**unchanged** engine runtime, byte-for-byte in the parts it reads.

### 2.2 The on-disk contract with the runtime (fixed, not ours to change)

`setupContainerWithoutCodegen` + `requireGeneratedFiles`
(`sdk/typescript/runtime/config.go:530`) pin the layout:

| Path | Read by the runtime as |
| --- | --- |
| `sdk/client.gen.ts` | required; presence checked with an actionable error |
| `sdk/` (whole dir) | mounted at `node_modules/@dagger.io/dagger` — **`sdk/` *is* the `@dagger.io/dagger` package** |
| `__dagger.entrypoint.ts` | required; the container entrypoint (`tsx --tsconfig tsconfig.json __dagger.entrypoint.ts`) |
| `tsconfig.json` | required for node/bun (passed to `tsx`) |
| `package.json` / `deno.json` | dependency install; not rewritten |

This kills the obvious simplification. Clients got to be a **scoped npm package**
depending on `@dagger.io/dagger` (§3.2 of `client-gen.md`); a module **cannot**,
because the runtime resolves that specifier to the module's own `sdk/`
directory. Modules stay on the **Bundle** origin: `sdk/` must contain a real,
self-contained copy of the library.

### 2.3 The four artifacts, and who makes them

For a node module, `GenerateDir` (`runtime_node.go:192`) + `Codegen`
(`main.go:77`) produce:

| Artifact | Produced by | Input it needs |
| --- | --- | --- |
| `sdk/client.gen.ts` + `sdk/<dep>.gen.ts` | `codegen generate-module` (Go) | module-facing introspection schema JSON |
| `sdk/index.ts`, `sdk/telemetry.ts` | static embeds (`tsutils/module/*`) | — |
| `sdk/core.js` (3.5 MB), `sdk/core.d.ts` (240 KB) | the **engine image** (`/bundled_lib`) | a `bun build` of the TS SDK library source |
| `__dagger.entrypoint.ts` | `ts-introspector` → `typedef.json` → `codegen generate-entrypoint` | the introspector binary, the `typescript` lib, the user's `src/`, and the generated `*.gen.ts` |
| `package.json`, `tsconfig.json` / `deno.json` | `tsutils` updators (gjson/sjson) | the existing files |
| lockfile | `npm`/`yarn`/`pnpm`/`bun` exec | network |
| `.gitattributes`, `.gitignore` | the **engine**, around codegen (`core/schema/modulesource.go:2630-2730`) | `VCSGeneratedPaths` / `VCSIgnoredPaths` |

Two details on the last row: `.gitattributes` gets `/<path> linguist-generated`
per generated path; `.gitignore` gets the ignored paths **minus** anything that
is also a generated path when the config is `dagger-module.toml` — i.e. for
workspace modules `sdk/` and `__dagger.entrypoint.ts` are meant to be
**committed**, and only `**/node_modules/**` / `**/.pnpm-store/**` are ignored.

### 2.4 The Go generator (`cmd/codegen`)

- `generate-module` → `TypeScriptGenerator.GenerateModule` → the same
  `generate()` as clients, targeting `<module-source-path>/sdk/src/api/client.gen.ts`
  inside the container, from which the caller then extracts `sdk/src/api`
  (`lib_generator.go:100`). The nesting is vestigial.
- Dependency splitting (`Exclude`/`Include` → one `<dep>.gen.ts` per dependency)
  is shared with clients and already ported here.
- `--bundle` flips `header.ts.gtpl` to `import { Context, BaseClient } from "./core.js"`.
- `generate-entrypoint` is pure file-to-file: `typedef.json` → `__dagger.entrypoint.ts`.
  It explicitly avoids dialing the engine.

The module bindings are generated from the **module-facing** schema (core + deps,
with `Host` and a few fields scrubbed — `core/moddeps.go:17-24`). The module's
*own* types are **not** in there: the self-call merge described in
`hack/designs/typescript-no-codegen-at-runtime.md` §4.3 was deferred and is still
not wired (no `Schema().Merge()` call exists in the runtime today). So the schema
we need is exactly `ModuleSource.introspectionSchemaJSON` — ungated, already
available. See §8 for how self-calls would land later.

### 2.5 The introspector

`ts-introspector` is a `bun build --compile` of
`sdk/typescript/src/module/entrypoint/introspection_entrypoint.ts`
(`toolchains/engine-dev/build/sdk.go:170`). Run as:

```
EMIT_TYPEDEF_JSON_FILE=/work/typedef.json \
  ts-introspector <moduleName> src sdk/client.gen.ts
```

It scans the user's TypeScript with the compiler API — so it needs the
`typescript` package on disk (`/src/node_modules/typescript`, mounted from the
engine image) and the generated `*.gen.ts` next to the client file to resolve
dependency-contributed types.

## 3. What is already in this repo

The client move brought most of the machinery over. `helpers/codegen` already
contains, unused, everything the module path needs on the Go side:

- `TypeScriptGenerator.GenerateModule` (`generator/typescript/generator.go:34`)
  and the `ClientGenFile = "client.gen.ts"` target.
- `ModuleGeneratorConfig` (`generator/config.go:34`), `Config.Bundle`.
- The `IsBundle` arm of `templates/src/header.ts.gtpl`.
- **The whole entrypoint renderer**: `generator/typescript/entrypoint.go`,
  `templates/entrypoint_typedef.go` (the `typedef.json` contract),
  `templates/entrypoint_functions.go`, `templates/src/entrypoint/*.gtpl`.
- Ported golden tests for the shared binding templates.

`helpers/config-updator` already has the module-flavoured `package-json`,
`tsconfig` and `deno-config` modes — written for `initModule`, but they are the
same edits module codegen needs, minus one or two keys.

`ModConfig.runtime` (`mod-config.dang:135`) already detects node/bun/deno from
the file layout, which is all the runtime detection codegen needs.

So the missing pieces are: a CLI mode, a dang implementation, and one real gap.

## 4. The toolchain: build the bundle in this repo, commit it

Everything above is portable except artifacts this repo has no way to produce,
all of which must match the library version the generated bindings are built
against:

- `core.js`, `core.d.ts` — the bundled `@dagger.io/dagger` library.
- the **introspector** — the compiled scanner (`ts-introspector` upstream).
- `typescript` — the compiler API the scanner imports (`import ts from "typescript"`).

They live in the engine image (`SDKSourceDir`: `/bundled_lib`,
`/bin/ts-introspector`, `/typescript-library`) and the engine hands them to
module-based SDKs as the `sdkSourceDir` constructor argument
(`core/sdk/module.go:114-120`). A `.dang` SDK installed in the workspace gets
nothing.

**Decided (2026-08-07, with the engine team): build them here.** A small
**packager** module in `.dagger/modules/packager` builds the bundle from the
TypeScript SDK library sources and commits the result into this repo, so the
`TypescriptSdk` module reads it straight off `currentModule.source` at generate
time. No engine primitive, no network at module-generation time, and no npm
dependency.

This is strictly better than the alternatives previously considered: an engine
field handing us `/bundled_lib` (keeps the library coupled to engine releases),
building from a remote ref on every generate (network + slow), or npm (blocked
anyway — `@dagger.io/dagger` publishes up to `0.21.8`, no `1.0.0-beta.*`).

It also lands something bigger than a workaround. In the no-runtime-codegen path
the engine mounts the module's **committed `sdk/`** as
`node_modules/@dagger.io/dagger` (`runtime_node.go:180`) — so once the bundle is
ours, **the library that executes user module code is ours too**, not the
engine's. That is the real end state for a repo called `dagger/typescript-sdk`,
reached incrementally.

### 4.1 What the packager builds

Mirroring `toolchains/engine-dev/build/sdk.go:155-190` (same bun image, same
commands, so the output is the one the engine ships today):

| Output | Command |
| --- | --- |
| `core.js` | `bun build ./src/index.ts --external=typescript --target=node` |
| `core.d.ts` | `tsc --emitDeclarationOnly` then `bun x rollup -c rollup.dts.config.mjs` |
| `introspector.js` | `bun build src/module/entrypoint/introspection_entrypoint.ts --target=node` |
| `index.ts`, `telemetry.ts` | static, ported from `tsutils/module/` |

Two deliberate differences from upstream:

- **`introspector.js`, not a compiled binary.** Upstream `bun build --compile`s
  the scanner because the engine image ships one executable per platform. A
  committed ~100 MB platform-specific binary is a non-starter for git; a bundled
  JS file is a few MB, platform-independent, and runs the same way
  (`bun introspector.js <name> src sdk/client.gen.ts`).
- **`typescript` is installed at generate time, not committed.** The scanner
  does `import ts from "typescript"`, and upstream keeps it external only
  because the engine mounts `/typescript-library`; we have no such mount. But
  the compiler is *our build-time dependency*, not the user's: the scanner is
  written against the API of the version the vendored library locks (6.0.3),
  which is a different version from the one a module declares for its own
  runtime (5.9.3, mirroring `tsdistconsts.DefaultTypeScriptVersion`). Carrying
  9.1 MB of third-party blob in git, re-committed on every engine bump, to
  serve one exec is the wrong trade. Codegen installs it instead, pinned so the
  layer is content-addressed on the version alone and shared across every
  module's generate.

  The version is **derived, not hand-written**: the packager reads it off the
  resolved install and writes `bundle/typescript-version.txt`, so re-vendoring
  cannot silently move the scanner onto a compiler API it was not written
  against. The cost is a registry fetch on a cold cache; if offline generation
  ever matters, committing the compiler again is one line behind the same seam
  (§5.4).

Both halves were validated end to end before anything depended on them: the
plain `bun build` scanner, run over a fixture module with our own `core.js` and
a module-style `client.gen.ts`, produced a `typedef.json` carrying the
per-declaration `location` data the entrypoint renderer needs.

**Two constraints on how the compiler is provided — both found the hard way,
and both must hold in the Phase 2 codegen container:**

1. **It must be the full package, not a trimmed one.** An earlier revision of
   this design proposed shipping `package.json` + `lib/typescript.js` (9.1 MB of
   the package's 24 MB). That loads, and scans trivial signatures, but the
   checker silently loses every *global* type: `lib/lib.*.d.ts` is missing, so
   `Promise`, `Array` and friends do not resolve. A module with
   `async foo(): Promise<string>` — i.e. most modules — fails with the
   misleading `could not resolve type reference for string`. Installing the
   package (§4.1) avoids this by construction.
2. **It must be resolvable from `introspector.js`'s own directory**, not the
   module's. Bare-import resolution walks up from the *importing file*, so
   putting the compiler in the module's `node_modules` does nothing for a
   scanner that lives elsewhere. Upstream sidesteps this by bundling the
   compiler into its `--compile`d binary and mounting the package only for the
   `lib.*.d.ts` files; we keep the compiler external, so the scanner and its
   `node_modules/typescript` have to sit together.

### 4.2 The library sources: vendored in-tree

**Decided: vendor.** `sdk/typescript/src/**` plus `package.json`,
`tsconfig.json`, `rollup.dts.config.mjs` and the lockfile are copied into this
repo and owned here, rather than fetched from a pinned upstream ref at build
time. That makes the packager hermetic and this repo the source of truth for the
library.

A copy has no merge base git can compute, so the one it would have had is
recorded by hand in [`library/VENDOR.md`](../library/VENDOR.md): the source tag
and commit, what was deliberately left behind, the exhaustive list of local
deltas, and the re-import procedure. Without it a re-vendor is a blind
overwrite.

It has one consequence worth planning for: the library ships its **own**
generated bindings at `src/api/client.gen.ts` (418 KB, committed upstream,
importing `../common/context.js`). Vendoring means eventually owning its
regeneration — the generator's `library` mode, which §6 therefore keeps.

**Copy how upstream already does it.** `.dagger/modules/typescript-client-dev/ts-sdk.dang:117`
is a dang `@generate` function — the same shape as our packager:

```
pub clientLibrary: Changeset! @generate {
  nodejsBase(...).withMountedDirectory(".", workspaceDir)
    .withExec(["yarn", "--cwd", sourcePath, "install"])
    # install a dagger client so codegen can reach the engine
    .withFile("/usr/local/bin/codegen", codegen(ws).binary)
    .withExec(["codegen", "generate-library", "--lang", "typescript", "-o", "./sdk/typescript/src/api/"])
    .withExec(["sh", "-c", "yarn … eslint --max-warnings=0 --fix ./src/api/"])
    .directory(".").changes(workspaceDir)
}
```

The schema is not a special field: with no `--introspection-json-path`, the
codegen binary runs a live `IntrospectionQuery` through its session
(`cmd/codegen/introspection/introspect.go` → `dag.Do(...)`,
`introspection.graphql`). A plain session serves no modules, so that *is* the
core-only unscrubbed schema — no synthesis, no stripping.

For us the only difference is where the session comes from: upstream installs a
dev-engine client, we dial the current engine with
`withExec([…], experimentalPrivilegedNesting: true)` (a documented arg on
`withExec`, so it is selectable from dang).

**Nesting here is fine.** The rule that codegen must not open a nested session
applies to the hot path — module generation, which every user hits on every
`dagger generate`. The packager runs when *we* bump the engine, in this repo.
To keep the boundary sharp, put the introspection query in a small separate
helper that only dumps JSON (upstream splits it the same way, as
`cmd/codegen introspect`), so `helpers/codegen` itself stays engine-free and
`library` mode keeps taking `--introspection-json-path`.

One sequencing note: upstream regenerates `src/api/client.gen.ts` continuously
(its git log is a stream of `regen` commits, and it moves with schema changes
like `feat(schema): require workspace arg on asSDK` #13845). So the first vendor
drop gets a current file for free, and `library` mode is what we need to
regenerate *independently* — against a dev engine, or once upstream stops
maintaining it. Its golden test is "reproduce the vendored file byte-for-byte".

### 4.3 Constraints on the packager

- **It must be a dang module.** A TypeScript module here would need the bundle
  that the packager produces — a bootstrap loop.
- **Determinism.** `dagger generate` in this repo re-runs it, so a non-reproducible
  build means a diff on every run. Pin the bun image by digest (as upstream
  does), pin the source ref, and commit the lockfile used for `bun install`.
- **Staleness check.** A `dagger check` that rebuilds and diffs against the
  committed bundle, so a stale artifact fails CI instead of silently shipping.
- **Marked generated.** `.gitattributes` `linguist-generated` for the bundle
  directory, mirroring what codegen does in user modules.
- **Cost of carry.** The committed bundle is **~9.5 MB**, measured on
  `v1.0.0-beta.9`: `core.js` 4.3 MB, `introspector.js` 4.3 MB, `core.d.ts`
  329 KB, plus 466 KB of library bindings. Everything in it is built from the
  vendored source; the one third-party piece, the TypeScript compiler, is
  installed at generate time instead (§4.1). Still worth refreshing on engine
  bumps rather than casually.

## 5. Target architecture

### 5.1 Data flow

```
dagger generate ──▶ TypescriptSdk.generateAllModule(ws)          [@generate]
                      │
                      └─ for each managed TS module (unchanged discovery):
                           stage local dependency closure          # unchanged
                           modSrc = polyfill.moduleSource(path).core
                           schema = modSrc.introspectionSchemaJSON  # module-facing
                           name   = modSrc.moduleOriginalName
                           src    = modSrc.sourceSubpath
                           │
                           ├─ 1. codegen container (Go helper, engine-free)
                           │     codegen module --introspection-json-path … \
                           │                    --module-name <name> --output /out
                           │   ⇒ client.gen.ts (+ <dep>.gen.ts)
                           │
                           ├─ 2. sdk/ assembly
                           │     committed bundle (core.js, core.d.ts, index.ts,
                           │     telemetry.ts) + the files from step 1
                           │
                           ├─ 3. entrypoint (two execs)
                           │     bun introspector.js <name> src sdk/client.gen.ts
                           │       ⇒ typedef.json
                           │     codegen entrypoint --typedef-json-path … --output /out
                           │       ⇒ __dagger.entrypoint.ts
                           │
                           ├─ 4. config-updator
                           │     module-package-json / module-tsconfig
                           │     (or module-deno-config for DENO)
                           │
                           └─ 5. .gitattributes / .gitignore lines
                      ▼
                   fork.withDirectory(sourcePath, …).changes  ⇒ Changeset
```

Steps 1–4 are ordinary containers. Nothing dials the engine; no
`experimentalPrivilegedNesting` anywhere.

### 5.2 Where the introspection JSON comes from

Upstream gets it handed in as an argument (`Codegen(modSource, introspectionJSON)`).
We read it off the API instead, and it is the **same schema, from the same
builder** — not an approximation:

| | Call |
| --- | --- |
| engine → runtime `Codegen` | `deps.SchemaIntrospectionJSONFileForModule(ctx)` (`core/sdk/module_code_generator.go:36`) |
| us | `ModuleSource.introspectionSchemaJSON` → `loadDependencyModules(src).SchemaIntrospectionJSONFileForModule(ctx)` (`core/schema/modulesource.go:3772`) |

The field is **ungated** (no `AfterVersion` view), so it works on today's engine;
the polyfill also surfaces it as `PolyfillModuleSource.introspectionSchemaJSON`
via `Module.introspectionSchemaJSON` (`core/schema/module.go:2699`, the same call
on `mod.Deps`). Either is fine; reading it off `modSrc.core` keeps the module
handle we already need for `moduleOriginalName` / `sourceSubpath` /
`engineVersion`.

Two practical notes:

- **Select `.contents`, don't pass the `File` around.** A `File` can't be
  projected as a handle inside a list projection; `generateAllClient` already
  selects `clientSchemaIntrospectionJSON.{{ contents }}` and writes it into the
  codegen container with `withNewFile`. Module codegen does the same.
- **Deps-only, both ways.** The engine appends the module's own types to that
  schema only when the SDK implements `ModuleTypes` (`typeDefsEnabled &&
  isSelfCallsEnabled`), and the TypeScript SDK dropped `ModuleTypes` — so
  `Codegen` receives deps-only today and so do we. If self-calls land later, the
  merge is `dag.schema(deps).merge(ownTypes, name).contents`
  (`core/schema/schematool.go`, gated `v1.0.0-0`) — callable from dang, so the
  codegen container still never dials the engine.

### 5.3 dang surface

`Mod.generate(ws)` keeps its signature and its dependency-closure staging; only
its body changes — from "ask the engine to generate" to "generate". Concretely,
in `typescript-sdk.dang`, mirroring the client helpers already there
(`generateClientBindings` / `configureClientNode` / `clientDirectory`):

- `let generateModuleBindings(schemaJSON, moduleName) → Directory!` — step 1.
- `let moduleSdkDirectory(bindings, toolchain) → Directory!` — step 2.
- `let generateModuleEntrypoint(moduleName, source, sdkDir, toolchain) → File!` — step 3.
- `let configureModuleNode(…) / configureModuleDeno(…)` — step 4.
- `let moduleDirectory(…) → Directory!` — the composition, the twin of
  `clientDirectory`.
- `let tsToolchain: Directory!` — **the single seam** for §4 (see 5.3).

`generateAllModule` itself is unchanged in shape: same discovery, same cwd
scoping, same `changes.{{isEmpty}}` concurrency trick, same fold onto
`pws.fork.changes`.

### 5.4 The toolchain seam

One private field resolves the bundle, and it is now trivial:

```
let tsBundle: Directory! { currentModule.source.directory("library/bundle") }
```

Everything downstream takes it as an argument rather than reaching for it, so if
the bundle ever moves (built on demand, fetched, shipped some other way) it is
one definition to change and no generator code.

### 5.5 Go helper CLI

`helpers/codegen` grows from one implicit mode to three explicit subcommands
sharing one flag set and one overlay writer:

```
codegen client     --introspection-json-path … --client-meta-path … --output …
codegen module     --introspection-json-path … --module-name … --output …
codegen entrypoint --typedef-json-path … --output … [--sdk-import …] [--source-dir …]
codegen library    --introspection-json-path … --output …          # §4.2, when we own the source
```

(The current CLI has no subcommand — client generation is the bare form. Adding
subcommands is a breaking change to a private interface with one caller, so it
is free; the dang side is updated in the same commit.)

`helpers/config-updator` gains `module-package-json` / `module-tsconfig` /
`module-deno-config`, ported from `tsutils/{package_json,tsconfig,deno_config}_updator.go`
with the tests, exactly as the `client-*` modes were.

## 6. Cleanup: what we do *not* carry over

The point of moving is to arrive with less code, not the same code in a new
place.

**Dropped from the Go generator:**

- `--lang` and the Go generator (already absent here).
- The `sdk/src/api` output nesting and `--module-source-path`: emit flat into
  `--output`, let dang place it. With it go `ModuleGeneratorConfig.ModuleSourcePath`,
  `.ModuleParentPath`, `.IsInit`, `.LibVersion` (all Go-only or vestigial), leaving
  `ModuleName`.
- `GenerateTypeDefs` (returns "not implemented").
- `Config.Bundle` as a flag — the `module` subcommand *is* bundle mode.
- `Config.IntrospectionJSON`, `Config.TypeDefsPath`, `NeedRegenerate`,
  `PostCommands` (Go-only; TS emits neither).
- `SDKLibOrigin` entirely: `Bundle` for modules, `@dagger.io/dagger` for clients,
  nothing to detect.

**Kept, contrary to the first draft of this doc:** `GenerateLibrary` and the
`../common/context.js` arm of `header.ts.gtpl`. That arm is not (only) the
retired `Local` lib origin — it is how the **library's own**
`src/api/client.gen.ts` imports its runtime. Vendoring the library (§4.2) makes
regenerating that file our job, so this mode goes from "delete it" to "build it
first" (§7 Phase 0). All three arms stay, one per mode: `library` →
`../common/context.js`, `module` → `./core.js`, `client` → `@dagger.io/dagger`.

**Dropped from the runtime port (never brought over):**

- `analyzeModuleConfig` (623 lines) collapses to "which runtime is this" via the
  existing `ModConfig.runtime`. Its other jobs are not codegen's business:
  `detectSDKLibOrigin` (Bundle-only now), `detectBaseImageRef` and
  `detectPackageManager` (runtime concerns — they stay in the engine's runtime,
  which is the only thing that acts on them).
- The `Codegen` default-`src/index.ts` seeding (`main.go:104`, itself marked
  *"TODO: handle that in an init method"*). `initModule` already owns templates.
- `wrappedSourceCodeDirectory` — the overlay re-shipped the user's own sources so
  `dag.currentModule().source()` saw them; a changeset carries only what changed.
- The per-runtime `GenerateDir` triplication (`runtime_node.go`,
  `runtime_bun.go`, `runtime_deno.go`): node and bun produce the *same* files
  (only the lockfile name differs, and the lockfile is going away), so this
  becomes one path plus a deno branch for `deno.json`.

**Dropped — lockfile generation** (decided). Today `GenerateDir` runs
`npm/yarn/pnpm/bun install --lockfile-only` and puts the lockfile in the
changeset. It needs the network, needs the package-manager detection we just
deleted, and is not codegen: a default module's dependencies are `{typescript}`,
which the runtime satisfies from its own mounted copy without installing
anything. With no codegen at runtime the runtime simply picks up whatever
lockfile is on disk, so the file is the user's to manage. `dagger generate` stops
creating/refreshing `yarn.lock`; existing lockfiles are untouched and still
honored.

Net: the module path we land is *the bindings generator + the entrypoint renderer
+ two JSON config writers*, and dang holds the composition — no lib-origin
branching, no package managers, no nested engine sessions.

## 7. Plan

**Phase 0 — the Go side.** `module` + `entrypoint` subcommands in
`helpers/codegen`, the trimming in §6, the module config writers in
`helpers/config-updator`, golden tests from a captured `introspection.json` +
`typedef.json`. No engine dependency; runs in `helperTestsCheck` today.

**Phase 1 — vendor + packager (§4).** Copy the library sources in (including
upstream's current `src/api/client.gen.ts`); `.dagger/modules/packager` (a
**dang** module — a TypeScript one would need the bundle it produces) with a
`@generate` function producing the bundle changeset; the committed `bundle/`
directory; the staleness check; the `.gitattributes` entry.

**Phase 1b — `library` mode.** The generator mode plus the small introspection
helper that dumps the session schema (§4.2), so we can regenerate
`src/api/client.gen.ts` ourselves instead of re-vendoring it. Golden test =
reproduce the vendored file. Not on the critical path for module codegen — it is
what makes the vendored library maintainable, and what we need the day upstream
stops regenerating it.

**Phase 2 — dang implementation, behind a differential check.** Implement
`moduleDirectory` and friends, but keep `Mod.generate` delegating to the engine.
Add an e2e check that generates the same fixture **both ways** and asserts the
trees are identical (modulo the known drops: lockfile, re-shipped sources). This
is the cutover's safety net and it is cheap while both paths exist.

**Phase 3 — cutover, for `dagger-module.toml` modules only.** Flip
`Mod.generate` to the local path when the module's config is
`dagger-module.toml`; keep delegating to `polyfill.moduleSource(...).generate`
for `dagger.json` ones (§8). Keep `generateLocalDependencies` staging (still
required: a dependent's schema can only be built if its local deps' generated
files exist, and dep generation may cross SDKs). Update the e2e assertions from
`sdk/index.ts` to the full expected tree, and add `__dagger.entrypoint.ts` +
`sdk/core.js` assertions. Verify `dagger call` on a generated fixture actually
runs — the runtime contract in §2.2 is only really proven by executing a module.

**Phase 4 — upstream cleanup** (separate `dagger/dagger` PR, §9).

## 8. Decisions

Settled by the constraints above:

- **Bundle origin stays.** Modules cannot be Remote/scoped packages while the
  runtime mounts `sdk/` as `@dagger.io/dagger` (§2.2). Consequence:
  `dagger generate` writes ~3.9 MB into each module (`core.js` 3.5 MB,
  `core.d.ts` 240 KB, `client.gen.ts` ~330 KB) and, for workspace modules, those
  are meant to be committed. That is today's behavior, unchanged — but worth
  stating out loud, because it is the strongest argument for eventually giving
  the runtime an npm-resolvable option.
- **Schema input is `ModuleSource.introspectionSchemaJSON`** (module-facing:
  core + deps, hidden types scrubbed) — provably the same file the engine hands
  to `Codegen` today (§5.2). Not `clientSchemaIntrospectionJSON` — that one hides
  nothing and promotes the module's own types to `Query`, which is right for
  clients and wrong here. The mirror image of the warning in `client-gen.md`
  §3.1. (The *library* mode is the exception and wants the unscrubbed one — §4.2.)
- **Entrypoint input stays `typedef.json`**, not the introspection schema: only
  the typedef carries the source-file `location` of each `@object` class, which
  the entrypoint needs to import them.
- **Self-calls stay out of scope** (they are not implemented upstream either).
  When they land, the merge (`dag.schema(deps).merge(ownTypes, name).contents`)
  can be done **in dang** and the merged JSON handed to the container — so the
  codegen helper stays engine-free. That is strictly nicer than upstream's plan
  of dialing the engine from inside the codegen container.

Decided 2026-08-07:

- **Toolchain (§4):** the packager builds the bundle here and commits it. This
  repo becomes the source of the library that both generates *and* executes
  workspace TypeScript modules.
- **Library sources: vendored in-tree** (§4.2), not fetched from a pinned ref.
  Consequence: `library` mode moves from "delete" to "keep" — regenerating
  `src/api/client.gen.ts` becomes ours eventually, done the way upstream already
  does it (live session introspection in the packager, §4.2), not by synthesizing
  a core schema from module-facing fields.
- **`typescript` ships in the bundle** (§4.1), trimmed to `package.json` +
  `lib/typescript.js` and mounted at `node_modules/typescript` for the
  introspector exec. Inlining it into the bundle has been tried and is
  problematic.
- **Lockfile generation: dropped** (§6). With no codegen at runtime, the runtime
  uses whatever lockfile is on disk; managing it is the user's call.
- **Scope: `dagger-module.toml` modules only.** Legacy `dagger.json` modules stay
  with the engine's built-in TypeScript SDK end to end, so `Mod.generate` keeps
  delegating for them. This is a choice for cleanliness, not a correctness
  requirement: generating a `dagger.json` module our way would not break it —
  the tree is the same shape, and the runtime regenerates everything at call
  time anyway (`useRuntimeCodegen` is true for that config format), so our output
  would simply be ignored at runtime. The differences are cosmetic — the engine
  writes `sdk/` + `__dagger.entrypoint.ts` into `.gitignore` for `dagger.json`
  modules (we would have to mirror that), we no longer emit a lockfile, and we
  do not seed `src/index.ts`. The one non-cosmetic wrinkle: the on-disk `sdk/` a
  user's editor typechecks against would carry *our* library version while the
  runtime regenerates with the engine's — harmless until they drift, and an
  argument for leaving legacy modules alone. Worth knowing: this repo's own
  `as-sdk.modules` list registers `dagger.json` fixtures (e.g.
  `fixtures/generate/app`), so the delegating branch is exercised from day one;
  if we later want a single path, those fixtures migrate to `.toml` rather than
  the SDK growing a second implementation.
- **All three TypeScript runtimes (node/bun/deno) in the first cut** — and it is
  nearly free, because the codegen output does not depend on the runtime at all.
  The bindings, the `sdk/` layout and the entrypoint are identical; the only
  branch is which config file gets written (`package.json` + `tsconfig.json` vs
  `deno.json`), in the last step of the pipeline. (The earlier "runtimes"
  question meant node/bun/deno coverage, not moving `ModuleRuntime`, which stays
  upstream.)
- **npm publishing stays with `dagger/dagger`** for now. It does not block
  modules (they use the committed bundle) and can be revisited once the library
  has actually moved; until then generated *clients* remain blocked on engine
  releases for a published `@dagger.io/dagger` (`client-bundle.md`).

Nothing is open on the design any more. What is left is empirical, and cheap to
settle before writing the real implementation (§7 would otherwise discover it
late):

1. ~~**Does the introspector run from a plain `bun build` bundle**~~ — **yes.**
   Bundled without `--compile`, with the trimmed compiler at
   `node_modules/typescript`, it scans a fixture module and emits a
   `typedef.json` with the `location` data the entrypoint needs.
2. **Does our `module` mode reproduce the engine's `sdk/client.gen.ts`
   byte-for-byte** for a fixture, given `ModuleSource.introspectionSchemaJSON`?
   This is the differential check of §7 Phase 2, run by hand once, first.
3. ~~**Is `bun build` output reproducible enough**~~ — **yes**, with the image
   pinned by digest and the vendored lockfile in place: a second packager run
   over an unchanged tree reports no changes. Dropping the lockfile is what
   breaks it (§4.2).

Only the differential check is left, and it belongs to Phase 2 anyway.
(For the record on the fetch alternative in §4.1: dang does support
`@cache(policy:, ttl:)` → `withCachePolicy`, but a plain container exec is
already content-addressed by the engine, so the decorator would mostly buy a TTL
rather than the caching itself.)

## 9. Upstream impact

What becomes deletable in `dagger/dagger` once this ships is **less than for
clients**, and that should be explicit rather than discovered later.

With the scope decision in §8 — CLI 1.0 modules here, `dagger.json` modules
upstream — the two implementations are **not** a duplication to be resolved; they
are the old and the new path, each owning one config format. Upstream's copy
retires when `dagger.json` does.

Deletable now:

- `SDKLibOrigin` `Local` end-to-end (already carrying a *"TODO: deprecate"*
  upstream) and `GenerateLocalLibrary`/`StaticLocalLib` — nothing selects it.

Deletable when `dagger.json` support ends:

- `TypescriptSdk.Codegen` and the per-runtime `GenerateDir`
  (`main.go:77`, `runtime_{node,bun,deno}.go`).
- `CreateOrUpdate*ForModule` + the `tsutils` module updators.

Not deletable, by design:

- The whole runtime path (`ModuleRuntime`, `setupContainerWithoutCodegen`, the
  package-manager plumbing, base-image detection).
- `cmd/codegen`'s TypeScript generator, `ts-introspector`, `/bundled_lib` and
  friends — still needed for runtime codegen on legacy `dagger.json` modules, and
  by the engine build regardless.

So the honest framing: this move is about **ownership and the CLI 1.0 path**, and
it accepts a two-copy period for legacy modules. What collapses the two copies is
the library source actually moving (§4.2), not this design.

### 9.1 The new coupling to watch

Once workspace modules run against *our* committed bundle rather than the
engine's `/bundled_lib`, library-vs-engine compatibility becomes ours to manage:
the bundle talks to the engine (session handshake, GraphQL, telemetry) and to the
generated entrypoint (`entrypoint()`, the registry, decorators). A module pinned
to engine X now runs a library snapshot chosen by *this repo*, not by X.

That is the same contract every non-builtin SDK already lives with (php, elixir,
java are git modules pinned per engine tag), and it is bounded by two things we
should keep true:

- refresh the bundle **on engine bumps**, from the matching upstream ref, and say
  so in the release notes;
- keep the e2e checks running a real `dagger call` against a generated fixture
  (§7 Phase 3) — that is the only test that actually exercises bundle ↔ engine
  ↔ entrypoint together.

Also keep `config-updator`'s `typescript` pin equal to
`tsdistconsts.DefaultTypeScriptVersion` (`5.9.3` today, and what we pin now):
when they match, the runtime mounts its prebuilt copy and skips dependency
installation entirely. Drift there silently turns every `dagger call` into an npm
install.

## 10. Key references

**This repo:** `typescript-sdk.dang` (`generateAllModule`, `codegenBuilder`,
`configUpdatorBuilder`, and the client helpers this mirrors), `mod.dang`
(`Mod.generate`), `mod-config.dang:135` (`runtime` detection),
`helpers/codegen/generator/typescript/{generator.go,entrypoint.go}`,
`helpers/codegen/generator/typescript/templates/src/{header.ts.gtpl,entrypoint/*}`,
`helpers/config-updator/main.go`, `design/client-gen.md`, `design/client-bundle.md`.

**Upstream `dagger/dagger`:**

- `sdk/typescript/runtime/{main.go,lib_generator.go,introspector.go,config.go,config_updator.go}`,
  `runtime_{node,bun,deno}.go`, `tsutils/**`.
- `cmd/codegen/{generate_module.go,generate_entrypoint.go,codegen.go}`.
- `core/sdk/utils.go:23` (`useRuntimeCodegen`), `core/sdk/module_runtime.go:50`,
  `core/sdk/module.go:114` (how module SDKs receive `sdkSourceDir`).
- `core/schema/modulesource.go:2630-2730` (`.gitattributes` / `.gitignore`),
  `core/moddeps.go:17-24` + `:160` (module-facing schema scrubbing).
- `toolchains/engine-dev/build/sdk.go:135-190` (how `core.js`, `core.d.ts` and
  `ts-introspector` are built — the recipe the packager mirrors),
  `sdk/typescript/{package.json,tsconfig.json,rollup.dts.config.mjs}`,
  `sdk/typescript/src/**` (the library sources), `runtime/tsutils/module/*`
  (`index.ts`, `telemetry.ts`, `core.d.ts` fallback),
  `runtime/tsdistconsts/consts.go` (bun image digest, `DefaultTypeScriptVersion`).
- `hack/designs/typescript-no-codegen-at-runtime.md` (why generate-time output is
  now the contract; the deferred self-call merge).
