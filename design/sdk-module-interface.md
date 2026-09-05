# Adopt the SDK-module interface: `findClientRoot` and `generateScope`

> **Status: implemented.** See `findClientRoot` and `generateScope` in
> `typescript-sdk.dang`, the `[sdks.typescript]` block in `dagger.toml`, the
> `e-2-e:scope:*` checks, and `.dagger/modules/engine-e2e`, which runs the suite
> against an engine built from the pull request this adopts. Landed in
> dagger/typescript-sdk#43.

Baseline: this document describes the TypeScript SDK at commit
`aedc08022eeddee4209e2aa6347c9786e7d46d34`. "Today" means that commit.

## Problem

This module is an SDK for the Dagger engine. Today the engine drives it through
an interface that dagger/dagger pull request 13992 ("Better SDK UX: modules-max
design", branch `sdk-ux-module-max`) removes.

Three engine APIs this module calls disappear in that pull request:

- `currentModule.asSDK(workspace:)`. `TypescriptSdk.modules` and
  `TypescriptSdk.generateAllClient` read the registered module and client lists
  through it.
- `ModuleSource.generateLocalDependencies`. `Mod.generate` and
  `TypescriptSdk.generateAllModule` stage a module's local dependency closure
  through it before running codegen.
- The `[modules.<name>.as-sdk]` block in `dagger.toml`. A `[sdks.<name>]` block
  replaces it.

Two functions this module exposes are no longer called:

- `initModule`, which the engine called for `dagger module init`.
- `initClient`, which the engine called for `dagger api client init`.

The pull request replaces all of it with two functions on the SDK module:

```
findClientRoot(ws: Workspace!): String
generateScope(ws: Workspace!, isModule: Boolean!, name: String!, clients: [ModuleSource!]!): Workspace!
```

`findClientRoot` answers with the workspace-root-relative path of the nearest
project root that contains the workspace working directory, or null when there
is none. The returned path must contain the working directory.

`generateScope` receives a workspace whose working directory is already the
scope. It produces the complete scope: the starter template and the module
configuration when the scope is new, and the generated bindings every time. The
`clients` argument carries the module sources the scope must have typed
bindings for.

A third function, `defaultModulePath(ws: Workspace!, name: String!): String!`,
is optional. This SDK does not implement it, so the engine places a new module
at `<directory holding dagger.toml>/.dagger/modules/<name>`.

The engine records each managed scope in `dagger.toml`:

```toml
[sdks.typescript]
module = "typescript-sdk"

[sdks.typescript.scopes."path/to/module"]
is-module = true
name = "my-module"
clients = ["./path/to/dependency"]
```

Without this change, this SDK stops working on the engine that ships pull
request 13992.

## Goals

1. Implement `findClientRoot` and `generateScope`.
2. Re-register this repository's own fixtures under `[sdks.typescript]`.
3. Keep standalone typed client generation working. TypeScript already has it;
   the adoption must not drop it.
4. Run the whole existing check suite against an engine built from
   `sdk-ux-module-max`, because no released engine can run it after this change
   (see "Which engine runs which check").
5. Update the README to the new command shapes.

## Non-goals

1. No compatibility adapter. Pull request 13992 removes the old interface
   outright, and this repository tracks it rather than bridging both.
2. No new client capability. A generated TypeScript client binds exactly one
   module (see [`client-gen.md`](./client-gen.md)); that constraint is kept, not
   widened.
3. No change to what generation writes. The generated tree of a module and of a
   client stays byte-identical to what this SDK writes today.
4. No engine fixes. Three engine defects on `sdk-ux-module-max` are worked
   around rather than repaired; they are listed under "Engine defects worked
   around".
5. No detection of a module from its own source tree when the two are split.
   A module whose configuration sets `source` to a directory outside the module
   root — the layout `dagger setup` migration writes, with configuration in
   `.dagger/modules/<name>/` and code in `ci/` — is not detected from `ci/`.
   `findClientRoot` there answers with `ci` itself, which is a TypeScript project
   and is where a client added from `ci/` belongs; it does not answer with the
   module whose configuration points at `ci/`. Address that module by path.

## Proposed approach

### `findClientRoot`

Truncate the workspace working directory at its first `node_modules` segment,
then walk up from there to the nearest directory holding a marker, and answer
with that directory. Answer `"."` at the workspace root, and null when no
marker is found.

The markers are:

- `package.json` — a Node or Bun project.
- `deno.json` — a Deno project.
- `dagger-module.toml` — a module in the current configuration format, but only
  when its `[runtime]` table names `typescript`.
- `dagger.json` — a module in the pre-1.0 format, but only when its `sdk.source`
  is `typescript`.

The nearest marker wins, whichever of the four found it. A `package.json` one
level up is not shadowed by a `dagger.json` two levels up. `configHitDepth` in
`typescript-sdk.dang` already ranks find-up hits this way for `mod`, and the
engine ranks its own `findRoots` hits the same way.

The two module-configuration markers are filtered by runtime because a Dagger
module configuration on its own says nothing about TypeScript. This repository
holds two counter-examples: `.dagger/modules/e2e/dagger.json` and the root
`dagger.json` are both Dang modules. Without the filter, a working directory
under `.dagger/modules/e2e/fixtures/` resolves to the Dang module at
`.dagger/modules/e2e`. `isTypescriptConfig` already implements the filter and
`mod` already uses it for the same purpose.

`deno.jsonc`, `bun.lock` and `jsr.json` are deliberately not markers.
`ModConfig.runtime` and `moduleDirectory` only read `deno.json`, so a scope
found by `deno.jsonc` is a scope this SDK cannot generate. `bun.lock` never
appears without `package.json`. Nothing here supports `jsr.json`.

The `node_modules` truncation is a deliberate divergence from the engine, whose
`findRoots` prunes `node_modules` only when walking down. Every installed
package carries a `package.json`, so without the truncation a working directory
inside `node_modules` resolves to a dependency of the project rather than to the
project. Truncating the search origin rather than retrying after a hit handles
nested `node_modules` in one step.

### `generateScope`, module scope (`isModule: true`)

1. Read the scope from `ws.cwd`, then move the working directory to the
   workspace root. The engine resolves a module's local dependencies to
   workspace-root-relative paths and reads them relative to `Workspace.cwd`, so
   at the scope's own working directory a dependency would be looked up under
   the module itself.
2. When the scope has no module configuration, render the starter template into
   it and write `dagger-module.toml` through the engine's manifest builder,
   `moduleManifest.withName(name).withLegacyTypescriptRuntime`. The template is
   merged onto whatever is already there: an existing `package.json`,
   `tsconfig.json` or `deno.json` keeps its own keys and gains only the keys
   Dagger requires. That is the behavior `initModule` has today.
3. When `clients` is not empty, replace the module's recorded dependencies with
   the client set, in the manifest file the module has — `dagger-module.toml`,
   or the pre-1.0 `dagger.json` for a module that has only that.
4. Generate the module. A module in the current configuration format is
   generated by this SDK; a pre-1.0 `dagger.json` module is still generated by
   the engine's built-in TypeScript runtime. That routing is unchanged.
5. Skip step 4 for an existing module that carries the generate skip marker. A
   new module is always generated.
6. Move the working directory back to the scope. The engine rejects a result
   whose working directory differs from the one it passed in.

Step 3 runs only for a non-empty client set, and that asymmetry is deliberate.
An empty `clients` list is not a statement that the module has no dependencies.
Every module written before `dagger module client add` existed has its
dependencies in its manifest and nothing in `dagger.toml`, including this
repository's own `.dagger/modules/e2e/fixtures/generate-deps/app`. Reconciling
from an empty list would delete those dependencies, and the module's bindings
for them, on the first `dagger generate` after the SDK is upgraded.

The cost of the asymmetry: removing the last client from a scope leaves the
dependency in the manifest. Removing it is then a manual edit. That is the
lesser harm, and it is reversible; deleting a dependency the user wrote is not.

### Clients in a module scope are the module's dependencies

A TypeScript module's dependency bindings and a standalone TypeScript client
are two different artifacts today:

- A dependency's bindings are one `<dependency>.gen.ts` file inside the
  module's own `sdk/` directory, rendered from the module's own introspection
  schema by `codegen module`. They import the bundled library from `./core.js`.
- A standalone client is a self-contained scoped npm package in its own
  directory: `dagger.gen.ts` for the core types, one `<module>.gen.ts` for the
  single module it binds to, plus `package.json` and `tsconfig.json`, rendered
  by `codegen client`. It imports `@dagger.io/dagger`.

In a module scope, `clients` is the new name for the first of those. Each entry
is recorded as a dependency in the module's manifest, and the module's own
codegen then emits its `<dependency>.gen.ts` from the module's introspection
schema, exactly as it does today for a manifest-declared dependency. No
separate client package is produced, and the generated tree is unchanged.

The engine's scope graph confirms the reading: the ordering edges between
scopes are built only from `SDKScope.Clients`
(`core/schema/workspace_sdk_generator.go` on `sdk-ux-module-max`), so a
dependency that is not also a client gets no generation ordering. dagger/dang-sdk
pull request 13, dagger/go-sdk pull request 37 and dagger/python-sdk pull
request 25 all read it the same way.

Note that `client-gen.md` in this directory describes a standalone client as
carrying one `<module>.gen.ts` per module in the bound module's *closure*. The
engine has since narrowed `ModuleSource.clientSchemaIntrospectionJSON` to the
core schema plus the single bound module, so a client today carries exactly one
module binding. This document states the current contract; `client-gen.md` is
superseded on that point.

### `generateScope`, non-module scope (`isModule: false`)

An empty `clients` list means there is nothing to generate; the workspace is
returned unchanged.

A non-empty `clients` list means a standalone typed client, and this SDK
generates one through the machinery it already has: `clientDirectory` for the
bindings and `configureClientNode` for the scoped package files, staged at the
scope directory. Regeneration keeps hand-written files in the directory and
removes generated module-binding files that are no longer bound, which is the
behavior `generateClient` has today.

`generateClient` takes the bound module as a workspace-relative path string.
`generateScope` receives a `ModuleSource` instead, so the path comes from
`ModuleSource.sourceRootSubpath` for a local source; a git source uses its
`asString` and `pin` and needs no path.

Exactly one client target per scope is supported. The generated client's
`serveBoundModule` bootstrap serves the single module the client binds to, and
two targets in one directory would collide on `dagger.gen.ts`, `package.json`
and `tsconfig.json`. A second target in the same scope is rejected with a
message naming the scope and telling the user to put the second client in its
own directory. The engine appends to a scope's client list without a cap, so a
user can reach the rejected state, and `dagger generate` then fails for the
whole workspace rather than for that scope alone.

python-sdk refuses standalone clients outright. TypeScript does not, because
TypeScript already has them and the adoption must not take a capability away.

### What is kept

`mod`, `Mod` and `ModConfig` keep their behavior and their signatures. Module
lookup already walks up for a module configuration file and validates that the
configuration declares the TypeScript runtime; it never went through
`currentModule.asSDK`. Configuration reading and editing, runtime detection,
template rendering, module codegen and client codegen are all untouched.

`Mod` gains one function, `generated(ws: Workspace!): Workspace!` — the
workspace with the module's generated files merged in, with no skip-marker
handling. `generate` becomes that, guarded by `skipGenerate` and diffed. The
existing signatures are left alone: dropping their redundant `ws` argument is
unrelated to this change and would churn every caller.

`Mod.generate` loses its dependency staging, because the API it staged through
is gone. What that costs is covered under "Dependency staging".

### What is removed

`modules`, `initModule`, `initClient`, `targetRuntime`, `generateAllModule` and
`generateAllClient`. The engine no longer calls any of them, and each of the
first four reads or serves an API that pull request 13992 deletes.

`modules` has no replacement. `dagger call typescript-sdk modules path` stops
working; the engine's own `dagger sdk scope list` lists the registered scopes.
A Dang replacement would have to select `Workspace.sdk`, which exists only on
the new engine, and the point of not selecting new-engine fields does not apply
here — the module already selects `moduleManifest`. It is left out because
nothing in this repository uses it and the engine now owns the listing.

### The init settings become constructor settings

`initModule`'s arguments have no function to live on any more. Under pull
request 13992 the engine turns an SDK module's constructor fields into flags on
`dagger module init <sdk>` and persists them under
`[sdks.<name>.scopes."<path>".settings]`. So `template`, `runtime`,
`packageManager` and `baseImage` become constructor fields of `TypescriptSdk`,
beside the existing `skipGenerateFilename`.

`runtime` becomes a `String!` accepting `node`, `bun` or `deno`, not the
`Runtime` enum. The engine coerces a
persisted setting into a constructor default through JSON
(`Module.ApplyWorkspaceDefaultsToTypeDefs`), and an enum-typed argument falls
into that function's catch-all branch: `deno` written in `dagger.toml` becomes
the JSON string `"deno"`, which is not a member of the enum, and the module then
fails to load rather than reporting a bad setting. A `String!` mapped to the
enum inside the constructor, with an explicit `raise` naming the accepted
values, turns that into an error the user can act on.

## Dependency staging

`Mod.generate` stages a module's local dependency closure before running the
module's codegen, through `ModuleSource.generateLocalDependencies`. A
dependency's schema is only loadable once the dependency's own generated files
exist, and `sdk/` is not committed for any fixture in this repository.

Pull request 13992 removes that API and moves the job to the engine: it folds
scopes leaf first and threads one workspace through them, so a scope listed as
a client of another scope is generated first.

That covers `dagger generate`. It does not cover a check that calls
`Mod.generate` on one module directly, which is what four checks in
`.dagger/modules/e2e/generate.dang` do against
`.dagger/modules/e2e/fixtures/generate-deps/app`:
`generateWorkspaceModuleCheck`, `generatePrunesStaleBindingsCheck`,
`generateRoutingCheck` and `generateDependencyCheck`. Each of them must stage
the dependency itself:

```
let staged = ws.withChanges(typescriptSdk.mod(ws, path: fixtures.depLibModule).generate(ws))
let changes = typescriptSdk.mod(staged, path: fixtures.depAppModule).generate(staged)
```

`.dagger/modules/runtimes/main.dang` already stages this way, for the same
reason.

## Which engine runs which check

The engine that runs `dagger check` for this repository today is the released
one, `v1.0.0-beta.11`. It has none of pull request 13992, and in particular has
no `moduleManifest`.

Dang infers a whole program on each call. Once `typescript-sdk.dang` selects
`moduleManifest` — which writing a new module's manifest requires — every call
into this module fails on the released engine, whatever the called function
touches. That is not something check selection can route around: a check that
only compares a constant field of this module fails too. dagger/python-sdk pull
request 25 records exactly this outcome, and dagger/dang-sdk pull request 13 and
dagger/go-sdk pull request 37 record it before that.

So after this change:

| Check group | Released engine `v1.0.0-beta.11` | Engine built from `sdk-ux-module-max` |
| --- | --- | --- |
| `e-2-e:*` except `sdk:helper-tests-check`, and `runtimes:*` | fail: every check calls into this module | pass |
| `e-2-e:sdk:helper-tests-check` | pass: runs `go test` over `helpers/`, selects nothing from this module | pass |
| `engine-e-2-e:*` | pass: builds the branch engine and drives it | not run there |

The failing checks are not disabled. A new module, `.dagger/modules/engine-e2e`,
builds an engine from `sdk-ux-module-max`, runs it as a playground container,
mounts this checkout into it, and runs the suite there:

- `engine-e-2-e:dev-sdk-check` drives the CLI: `dagger sdk list`, `dagger module
  init typescript`, and `dagger call` on the module it creates.
- `engine-e-2-e:checks-check` runs `dagger check "e-2-e:**" "runtimes:**"` inside
  the playground, so the whole suite runs against an engine that has the
  interface.

Three mechanics that check depends on, none of them optional:

- The groups are named. A bare `dagger check` inside the playground would find
  `engine-e2e` among the workspace's modules and build a second engine inside
  the first, recursing; it would also run `packager:*`, which opens a further
  nested privileged session; and it would make the inner engine clone
  dagger/dagger to load `engine-e2e`.
- `.git` is excluded from the mounted checkout and `git init` runs inside it. In
  a git worktree `.git` is a file pointing at a directory the container does not
  have, which leaves every git call following a dangling pointer. The engine
  accepts an unborn repository, so nothing is committed.
- The playground is given the pinned engine commit as its `version`. Left unset,
  `EngineDev.Service` names the engine's state cache volume with a random
  suffix, so every run starts an engine with an empty cache.

python-sdk pull request 25 ran its suite in its playground by hand and reported
the result in the pull request body. Making it a check is the one place this
adoption goes further than its precedent: the coverage is a gate rather than a
note.

The released-engine failures stay red until an engine with pull request 13992
ships. The README says so, and says which checks are expected to fail and why.

## Engine defects worked around

Three defects on `sdk-ux-module-max` shape what the checks can do. All three are
reported against dagger/dagger pull request 13992 by dagger/python-sdk pull
request 25, whose body describes each in full. None is this SDK's to fix.

1. `dagger module client add <target>` prints "no changes to apply" and writes
   nothing, for every SDK. The client behavior is checked by calling
   `generateScope` directly from Dang instead, and the checks never drive that
   command.
2. `dagger generate` fails before reaching the SDK when a module scope records
   a client, with `cannot instantiate dagql.Class[*core.GeneratorGroup] with
   dag`. No check runs a workspace-wide `dagger generate`, which is what makes
   the recorded client edge safe rather than incidental: `dagger check` builds
   its checks from each module's own tree and never reaches
   `runSDKModuleGeneratorGraph`, which only `dagger generate` and `dagger module
   client update` call. The client-to-dependency behavior is covered by calling
   `generateScope` directly. Until the defect clears, the recorded edge on
   `generate-deps/app` is configuration nothing exercises.
3. SDK settings persisted on a scope are not delivered to the provider
   constructor. `engine-e-2-e:dev-sdk-check` therefore asserts only the default
   template and runtime; the non-default settings are covered by constructing
   `TypescriptSdk` with them directly from Dang, which exercises the generator
   but not the engine's setting delivery.

Each defect is re-tested during implementation against the pinned commit, and
any that no longer reproduces gets its full check rather than the workaround.

## Affected components

| Path | Change |
| --- | --- |
| `typescript-sdk.dang` | add `findClientRoot` / `generateScope`; turn the init arguments into constructor settings; remove `modules`, `initModule`, `initClient`, `targetRuntime`, `generateAllModule`, `generateAllClient` |
| `mod.dang` | add `generated`; drop the dependency staging |
| `dagger.toml` | replace `[modules.typescript-sdk.as-sdk]` with `[sdks.typescript]` and one `[sdks.typescript.scopes."…"]` per fixture; add `[modules.engine-e2e]`; remove `[modules.sdk-sdk]` |
| `dagger.lock` | drop the `sdk-sdk` entries; add what `engine-e2e` resolves |
| `.dagger/modules/e2e/*.dang` | replace the discovery checks with scope checks; replace the init checks with `generateScope` checks; stage the dependency in the four checks that generate a module with one |
| `.dagger/modules/runtimes/main.dang` | remove `invokesFunctionCheck`, whose released-CLI harness drives the removed init contract |
| `.dagger/modules/engine-e2e/` | new module: the two dev-engine checks |
| `README.md` | new command shapes, the scope model, and which checks fail on the released engine |

## Alternatives considered

**Write `dagger-module.toml` without the engine's manifest builder.** Rendering
a new manifest from a string, and adding a `dagger-module.toml` dependency
editor to the existing `helpers/module-config` Go helper, would keep this module
free of new-engine selections — and that, not convenience, is the real prize: it
is the only way the released-engine checks stay green. Rejected anyway. It buys
green on an engine that is being replaced, at the price of owning a TOML writer
for a format the engine owns, indefinitely. `moduleManifest` exists in pull
request 13992 for exactly this call site, and the three sibling SDK adoptions
all use it.

**Re-implement dependency staging inside `generateScope`.** Generating each
local TypeScript client module first, and staging the result, would restore what
`generateLocalDependencies` did. Rejected: the engine's scope graph already
orders this for `dagger generate`, transitive closures would have to be walked
by hand, and the staged tree would then have to be subtracted back out of the
result. The four checks that need staging do it themselves in one line.

**Refuse standalone clients, as python-sdk does.** Rejected: TypeScript has had
them since `client-gen.md` landed, users have them registered today, and
`.dagger/modules/e2e/client.dang` is existing coverage that would be thrown
away.

**Generate several clients in one scope.** Rejected: the generated bootstrap
serves one bound module, the three package files collide, and each client's
introspection schema is core plus one module, so there is nothing to merge.
Supporting N is a codegen redesign. One scope per client is the model the engine
already supports.

## Testing

Every check below runs on an engine built from `sdk-ux-module-max`, through
`engine-e-2-e:checks-check`. On the released engine they fail, as explained
above.

`findClientRoot`:

- finds the nearest `package.json` and the nearest `deno.json`;
- finds a TypeScript module by its `dagger-module.toml` and by its
  `dagger.json`;
- does not answer with a Dang module — from
  `.dagger/modules/e2e/fixtures/client/out` it must not return
  `.dagger/modules/e2e`;
- prefers the nearer of two markers of different kinds;
- answers `"."` at the workspace root and null where there is no marker;
- answers with the project rather than an installed package when the working
  directory is inside `node_modules`, including nested `node_modules`.

`generateScope`, module scope:

- an existing module with no clients gets its generated tree and nothing else,
  and the working directory is unchanged;
- an existing module with a hand-written dependency and no clients keeps that
  dependency in its manifest;
- a scope with no configuration gets the template, a `dagger-module.toml` with
  no `dagger.json` beside it, and the generated tree; existing files at the
  scope survive;
- the template, the runtime, the package manager and the base image follow the
  constructor settings;
- a module with one client records it in the manifest and gains its types in
  the generated bindings;
- an existing module with the skip marker is left alone, and a new one is
  generated regardless.

`generateScope`, non-module scope:

- no clients is a no-op;
- one client writes a complete client package, preserves a hand-written file,
  and prunes a stale binding;
- two clients are rejected.

Module lookup, configuration, module codegen, client codegen and module
execution keep their existing checks, with the dependency staged in the four
checks that need it.

`engine-e-2-e:dev-sdk-check`, driving the CLI in the playground:

- `dagger sdk list` lists `typescript`;
- `dagger module init typescript --name … --path …` writes
  `dagger-module.toml`, `package.json`, `tsconfig.json`, `src/index.ts`,
  `sdk/client.gen.ts` and `__dagger.entrypoint.ts`, and no `dagger.json`;
- `dagger call` on the new module returns a value, so the generated tree loads
  and dispatches.

## Risks

**The branch advances beyond the pinned commit.** `sdk-ux-module-max`
force-pushes and its API moves, including the part of it this document
describes. Between `055def6f7794b9d0dfdcc4252f0695ea92257536` (what python-sdk
pinned) and `64f52032c9962f2493a5499bf7320bb568c5d6a4` the manifest dependency
functions were renamed from `withDependency` / `withoutDependency` to
`withLegacyRuntimeDependency` / `withoutLegacyRuntimeDependency`. Between that
and `e08f10155304e33e3f0d84d33ae73cb64277f4f2` the detection function itself
was renamed, from `detectScope` to `findClientRoot`, and between that and
`7e6fc93c86f0bf0eebae8c2c74a249c3b5cc451f` its return type became nullable, so
an SDK reports "no client root" with null rather than an empty path. Each time
the branch was also rebased, so every commit before the change has a new hash. Two places carry the pin: the `engine-dev`
dependency in `.dagger/modules/engine-e2e/dagger-module.toml`, and the
`engineCommit` field in `.dagger/modules/engine-e2e/main.dang`. Bumping means
changing both, then re-running `engine-e-2-e:*`.

**The whole suite runs in one nested engine.** `engine-e-2-e:checks-check`
builds an engine and runs the entire check suite inside it, so it is slow and
one failure hides the rest until it is opened. `engine-e-2-e:dev-sdk-check` is
kept separate so the smallest end-to-end signal does not depend on the slowest
check.

**Reaching a package registry from the nested engine is unreliable.** Five runs
of `engine-e-2-e:checks-check` produced four different failing sets, every
failure a `yarn` or `npm` connection timeout and every one of them in a check
that installs packages: the pre-1.0 fixtures, which the engine's built-in
TypeScript runtime generates with `yarn install`, and the client compile check,
which runs `npm install`. Every check passed in at least one run, and no failure
was ever an assertion. The engine runs two levels of network translation deep
here, so the check may need a re-run rather than a fix.

**Released-engine checks stay red until the engine ships.** Thirty-odd checks
fail for one reason, which is the state in which a real regression is easiest to
miss. The mitigation is that the same checks run green in the playground, so a
regression shows up there.

**A module with a local TypeScript dependency cannot be generated on its own
from a clean checkout.** `sdk/` is not committed, so the dependency's schema is
only loadable once the dependency has been generated. `dagger generate` handles
this through the scope graph. `dagger call typescript-sdk mod --path … generate`
on a dependent, before its dependency has ever been generated, does not, and
fails with a module-load error rather than a message naming the cause.

## Implementation plan

Four patches, in order. Each leaves every module in the workspace loadable.

### 1. `sdk: replace the init contract with findClientRoot and generateScope`

One patch for the SDK surface and its callers together: `typescript-sdk.dang`
removes functions that `.dagger/modules/e2e` and `.dagger/modules/runtimes`
call, so splitting them leaves a workspace that does not load.

`typescript-sdk.dang`:

- Add constructor settings `template: String! = "default"`,
  `runtime: String! = "node"`, `packageManager: String! = ""`,
  `baseImage: String! = ""`, and a private mapping from the `runtime` string to
  the `Runtime` enum that raises on an unknown value.
- Add `findClientRoot`, with private helpers for the `node_modules` truncation and
  for the runtime-filtered configuration markers. Rank hits with the existing
  `configHitDepth`.
- Add `generateScope`, with private helpers `hasModuleConfig`, `scopeHasFile`,
  `withClientDependencies`, `dependencySource`, `pathParts` and `relativePath`.
- Remove `modules`, `initModule`, `initClient`, `targetRuntime`,
  `generateAllModule`, `generateAllClient`, and `clientCwd` / `inCwdScope`,
  which only served the two removed rollups.

`mod.dang`: add `generated(ws: Workspace!): Workspace!`; make `generate` use it;
drop `generateStaged` and the `generateLocalDependencies` staging.

`.dagger/modules/e2e/`:

- `discovery.dang` is replaced by `scope.dang`, holding the `findClientRoot`
  checks.
- `init.dang` becomes the `generateScope` checks. The template, runtime,
  package-manager and base-image assertions move onto the new-scope path, where
  they now come from constructor settings.
- `generate.dang` stages the dependency in the four checks that generate
  `generate-deps/app`, `generateDependencyCheck` among them;
  `generateLegacyDependencyCheck` keeps its subject, which routes to the engine
  and never needed staging. `generateAllCheck` and `generateAllScopeCheck` go,
  and `generateRoutingCheck`'s `generateAllModule` half is replaced by the same
  assertion through `generateScope`.
- The `deps/app` fixture goes with `generateAllCheck`, its only reader.
- `client.dang` gains the non-module-scope checks and keeps the direct
  `generateClient` ones; `generateAllClientCheck` becomes the
  `generateScope`-driven equivalent.
- `sdk.dang` drops `targetRuntimeCheck`.
- `util.dang` drops `managedModules` and `discoverySnapshot`, and gains the
  paths the new checks need.

`.dagger/modules/runtimes/main.dang`: remove `invokesFunctionCheck`, whose
sdk-sdk harness drives a released CLI through the removed init contract.

### 2. `workspace: register the SDK under sdks.typescript`

`dagger.toml`:

- `[sdks.typescript] module = "typescript-sdk"`.
- One `[sdks.typescript.scopes."<path>"]` per fixture that was a
  `[[modules.typescript-sdk.as-sdk.modules]]` entry, with `is-module = true` and
  the module's name read from its configuration file.
- `generate-deps/app` records its dependency as a client, which is the
  configuration `dagger module client add` produces and what orders the
  dependency's scope first. `generate-legacy-deps/app` does not: a pre-1.0
  module is regenerated by the engine's own runtime at load time, so its
  dependency's schema loads without anything having generated it, and recording
  the edge would only make generation reserialize a hand-written `dagger.json`.
- The client fixture becomes a non-module scope at
  `.dagger/modules/e2e/fixtures/client/out`, with the bound module as its single
  client.
- Remove `[modules.sdk-sdk]`, now that nothing selects `sdkSdk`.

`dagger.lock`: drop the `sdk-sdk` entries.

### 3. `e2e: check the SDK against an engine built from sdk-ux-module-max`

New `.dagger/modules/engine-e2e/` with `dagger-module.toml` and `main.dang`,
depending on `github.com/dagger/dagger/.dagger/modules/engine-dev` pinned to
`7e6fc93c86f0bf0eebae8c2c74a249c3b5cc451f`, and `[modules.engine-e2e]` in
`dagger.toml`. Two checks, `devSdkCheck` and `checksCheck`, as described under
"Which engine runs which check". The module is created and registered in the
same patch, so `dagger.toml` never names a directory that does not exist, and
`dagger.lock` gains what the new dependency resolves to.

### 4. `docs: document the scope model`

`README.md` rewritten around `dagger module install`, `dagger module init
typescript --name … --path …`, `dagger module client add`, scopes in
`dagger.toml`, the two check groups, and which checks fail on the released
engine and why.

## Progress

This section is run state, not design. It records where the work is so a
restarted run does not replay it.

- Phase 0, orientation: done. Repository `dagger/typescript-sdk`, base `main`,
  design home `design/`, patches managed with Stacked Git, sign-off
  `Signed-off-by`, host GitHub, checks run on dagger.cloud.
- Phase 1, feature doc: done.
- Phase 2, implementation plan: done.
- Phase 3, adversarial plan review: done, two rounds. Round one refuted the
  first version's claim that the released-engine checks could stay green; this
  version drops that claim. Round two caught that a bare `dagger check` in the
  playground recurses into itself.
- Phase 4, implementation: four patches on the branch — the SDK surface and its
  callers, the `dagger.toml` registration, the branch-engine check module, and
  the README.
- Phase 5, code review: done, one round, two reviewers and a separate fixer.
- The whole suite passes against an engine built from the pinned commit:
  44 checks in `e-2-e:*` and `runtimes:*`, plus `engine-e-2-e:dev-sdk-check`.
  No single run has had all 44 green at once on the current pin; every check has
  passed, and every failure has been a package-registry timeout in the nested
  engine rather than an assertion.
- `sdk-ux-module-max` head when this was written, and the pin:
  `7e6fc93c86f0bf0eebae8c2c74a249c3b5cc451f`.
- Phase 6, draft pull request: dagger/typescript-sdk#43, opened as a draft at
  head `9d9eefc2f923c5f5f22fc2b6d8463bf70bdee514`.
- Phase 7, checks: at the expected end state. The four that can pass on the
  released engine pass — `load`, `e-2-e:sdk:helper-tests-check`,
  `engine-e-2-e:dev-sdk-check` and `engine-e-2-e:checks-check` — along with the
  four `packager:*` checks, which select nothing from this module. Everything
  else in `e-2-e:*` and `runtimes:*` fails there for the one reason this
  document gives, and passes inside the playground engine.
