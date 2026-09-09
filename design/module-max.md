# Adopt the module-max SDK interface

Status: implemented and verified against a dev engine at `8fd9b22b`. Tracks [dagger/dagger#13992](https://github.com/dagger/dagger/pull/13992)
("Better SDK UX: modules-max design"), whose source of truth is
[`future/cli-1.0.md`](https://github.com/dagger/dagger/blob/sdk-ux-module-max/future/cli-1.0.md)
on branch `sdk-ux-module-max`. Engine line references below are from that branch.

That branch rebases and force-pushes, and it has changed the SDK contract more
than once since this document was first written. `.dagger/modules/engine-e2e`
pins the commit and drives a real CLI against it, so the next change surfaces as
a failing check rather than as a provider the engine quietly stops recognizing.

Companion to [`module-gen.md`](./module-gen.md) and [`client-gen.md`](./client-gen.md),
which moved module and client codegen into this repo. Those moves are **not**
undone: the codegen machinery they built is exactly what survives. What changes
is the *interface the engine calls it through*, and the *unit of work* it is
called with.

## 1. What the engine replaces

The PR deletes the beta SDK-module interface with **no compatibility adapter**
(non-goal 1 in `cli-1.0.md`). Every entry point the engine currently uses to
reach this SDK is removed:

| Today (beta) | Module-max |
| --- | --- |
| `currentModule.asSDK(ws).modules` / `.clients` | Removed. The engine reads scopes from `dagger.toml` and calls us once per scope. |
| `generateAllModule` / `generateAllClient` `@generate` rollups | Removed. The engine synthesizes `typescript-sdk:generate` and **ignores `@generate` from a module in the provider role** (`core/schema/workspace.go:3368`). |
| `initModule(ws, name, path, template, runtime, packageManager, baseImage) -> Changeset!` | Folded into `generateScope`; the extra arguments become constructor settings. |
| `initClient(ws, path, module, dev) -> Changeset!` | Gone. Client registration is engine-owned; we contribute nothing. |
| `ModuleSource.generateLocalDependencies` | Removed. The engine orders scopes itself (`core/schema/workspace_sdk_generator.go:319`). |
| `[modules.typescript-sdk.as-sdk]` in `dagger.toml` | `[sdks.typescript]` + `[sdks.typescript.scopes."<path>"]`. Any config rewrite strips the old sections (`core/workspace/config_document.go:383`). |
| `generateClient(ws, module, path)` — caller picks the output directory | `dagger module client add <MODULE>` has **no path argument**. The SDK picks the layout. |

`initModule`, `initClient` and `targetRuntime` are named as "staying" in
`cli-1.0.md`, but only as part of the **legacy runtime** interface
(`core/sdk/consts.go`) — the one a module implements when it is named in
`dagger-module.toml`'s `[runtime] source`. That is not the role this repo plays:
we are an *installed provider* that writes files, and the engine executes what we
write with its builtin `typescript` runtime. So for us those three functions are
dead weight after the move.

### The new required surface

```graphql
findClientRoot(ws: Workspace!): String                      # nullable
generateScope(ws: Workspace!, isModule: Boolean!, name: String!, clients: [ModuleSource!]!): Workspace!
defaultModulePath(ws: Workspace!, name: String!): String!   # optional
```

Validation is **exact**: function name, argument count, argument order, argument
types and return type all have to match (`core/sdkmodule/provider.go:264`). A
provider that fails validation is not an SDK — `dagger module install` will not
record `[sdks.<name>]` for it (`core/schema/workspace_install.go:48`). So:

- No extra arguments on `generateScope`. Every knob moves to constructor settings.
- `Changeset!` returns become `Workspace!` returns.

The reference implementation is `modules/sdk-ux-go/main.dang` in the PR — 101
lines, worth reading before this document's §3.

## 2. What survives untouched

Everything below the interface:

- `helpers/codegen` (module bindings, entrypoint, client bindings), `helpers/config-updater`,
  `helpers/module-config`, `helpers/render-template`.
- `library/bundle` and the `introspector` container, `tsBundle`, `moduleSdkDirectory`,
  `generateModuleEntrypoint`, `moduleDirectory`, `clientDirectory`.
- `templates/`, `Template`, `ModConfig` and the `Runtime` enum.
- `targetRuntime`'s **value** (`"typescript"`), now written by us into the
  manifest instead of by the engine.
- `Mod` / `ModConfig` as a user-facing surface (`dagger call mod --path=… config set …`).
  Normal module functions, unaffected by the provider role.

The rewrite is concentrated in `typescript-sdk.dang`'s top layer and in
`mod.dang`'s `generate` routing. The engine room does not move.

## 3. The new SDK surface

### 3.1 `findClientRoot`

Answers "which directory is the TypeScript project containing `ws.cwd`". Used
only for **clients** (`module init` never calls it — `cli-1.0.md` §findClientRoot
table). Must return a workspace-root-relative parent of `ws.cwd`, or `null`
(`core/schema/workspace_sdk_module.go`).

```dang
findClientRoot(ws: Workspace!): String {
  let found = scopeMarkers.reduce(null) { acc, name =>
    let hit = ws.findUp(name)
    if (configHitDepth(hit) > configHitDepth(acc)) { hit } else { acc }
  }
  if (found == null) { null } else { configDir(found) }
}
```

with `scopeMarkers = ["package.json", "deno.json", "deno.jsonc", "tsconfig.json"]`.
Deepest hit wins, reusing the `configHitDepth` ranking already in
`typescript-sdk.dang:144`.

Two consequences worth stating up front:

- **You cannot create the first TypeScript client scope in a directory with no
  TypeScript project.** `findClientRoot` returns `null`, and `dagger module client add`
  fails with "client generation is not available from workspace cwd". `--sdk=typescript`
  does not help: it selects *which* provider is asked, not what it answers. This is
  defensible (a TypeScript client belongs in a TypeScript project) but it is a
  behavior change from `generateClient(module, path)`, which created the directory.
  Escape hatch: a `scopeMarkers` setting so a repo can add its own marker.
- **Root-level ties are an error.** A repo with `package.json` and `go.mod` both at
  the root makes TS and Go detect the same scope, and the engine refuses:
  "SDKs %q and %q detect the same scope; specify --sdk"
  (`core/schema/workspace_sdk_module.go:710`). Users pass `--sdk`; nothing we can
  do from here.

### 3.2 `defaultModulePath`

**Recommendation: do not implement it in v1.** The engine default
(`<active-config-parent>/.dagger/modules/<name>`) is what we already produce, and
`cli-1.0.md` is explicit that the choice is persisted once and never revisited —
an SDK update cannot move an existing module. Shipping without it keeps that
decision open. The optional function can be added later without breaking the
interface (`core/sdkmodule/provider.go:293` marks it `optional: true`).

### 3.3 `generateScope`

One function, called once per scope, with the **complete desired state** of that
scope. It replaces `initModule`, `generateAllModule`, `generateClient` and
`generateAllClient`.

```dang
generateScope(
  ws: Workspace!,
  isModule: Boolean!,
  name: String!,
  clients: [ModuleSource!]!,
): Workspace! {
  let withModule = if (isModule) { moduleFiles(ws, name) } else { ws }
  clientPackage(withModule, clients)
}
```

The engine's guarantees around the call (`core/schema/workspace_sdk_module.go:892`):

- `ws.cwd` **is** the persisted scope. The scope directory is created empty
  before the call if it does not exist (`:933`), so a fresh module can inspect
  its own directory before writing.
- We may write anywhere in the workspace, not just under the scope (`cli-1.0.md`
  §Responsibility split). **We decline that permission.** It exists for SDKs whose
  project file sits above the module — the Go SDK updating a `go.mod`/`go.sum`
  above the scope. For TypeScript the scope *is* the project root by construction:
  `findClientRoot` returns the directory holding `package.json`, so nothing we
  generate belongs above it. Staying inside also keeps one generation reviewable
  as one directory.
- We must not touch the active `dagger.toml` (`:1035`) and must not change
  `ws.cwd` (`:1018`).
- When `isModule`, a valid module config must exist at the scope afterwards
  (`:1044`), and **writing it is ours** (§4.6). The engine does not create it
  before the call.

### 3.4 Settings

Constructor arguments become settings, and the CLI flattens them into flags
(`internal/cmd/dagger/module_sdk_dynamic.go:182`). Precedence is
constructor default → `[modules.typescript-sdk.settings]` → `[sdks.typescript.scopes.<S>.settings]`
(`core/schema/workspace_sdk_init.go:56`).

```dang
type TypescriptSdk {
  runtime: String! = ""            # empty = detect; parsed into the internal Runtime enum
  template: String! = "default"
  packageManager: String! = ""
  baseImage: String! = ""
  # existing: targetEngineVersion
  # private (let): moduleConfigFilenames, scopeMarkers
}
```

The default is empty, not `"node"`, and that is load-bearing: empty means
"detect from the config files already in the scope" — `deno.json` for Deno, a bun
lockfile for Bun, Node otherwise — so adopting this SDK, or regenerating a module
created before the setting existed, does not silently move a Bun or Deno project
onto Node. Setting it explicitly is what moves a scope to another runtime.

giving `dagger module init typescript --runtime=bun --template=empty` and
`dagger module client add foo --typescript-runtime=bun`, persisted into
`[sdks.typescript.scopes."<path>".settings]`.

`cli-1.0.md` uses this SDK as its example of a settings-driven provider:

```text
typescript-sdk  runtime  node  JavaScript runtime for generated code
```

Only knobs a user should turn are settings. The config-filename lists become
private `let` bindings — they are internal constants, and as settings they would
show up as noise in `dagger module settings` and as flags on `module init`.

**Decided: `runtime` ships as a validated `String!`, not the `Runtime` enum.**
The enum stays internal (`ModConfig.runtime`, `moduleDirectory`); only the
setting is a string we parse. `addSDKModuleSettingFlags`
(`internal/cmd/dagger/module_sdk_dynamic.go:182`) *silently skips* argument types
it cannot turn into a flag, so an enum that does not register would surface as a
missing `--runtime` flag rather than an error — not a failure mode worth taking
on the critical path. Revisit once the whole path works end to end.

## 4. The design problems this creates

### 4.1 Where do N clients go in one scope?

The biggest change. Today `generateClient(ws, module, path)` writes a
self-contained scoped npm package at a caller-chosen `path`, bound to exactly one
module. Module-max gives us a scope — typically the user's own project directory,
with the user's own `package.json` — and a *set* of targets, with no path.

**Decided: one generated package per scope, never per target, and never the
user's own package.** The core bindings are the bulk of a generated client, so
one `dagger.gen.ts` per scope plus one `<target>.gen.ts` per client. `clients` is
"the complete desired client set", so `dagger module client rm` is just
regeneration without that file. Where the package lands depends on what the scope
is:

| Scope | Client output | Package files |
| --- | --- | --- |
| Module (`is-module = true`) | `clients/`, beside the generated `sdk/` | self-contained: `clients/` gets its own generated `package.json` and `tsconfig.json`, same as a client-only scope |
| Client-only | `.dagger/clients/` | self-contained: its own `package.json` and `tsconfig.json`, generated |

#### A module scope's clients are generated twice

A target in a **module** scope is rendered into two places, because it has two
callers.

`clients/` is for code outside the module: a standalone package carrying the
`serveBoundModule` bootstrap that installs its targets before the caller's
callback runs.

`sdk/<target>.gen.ts` is for the module's own source. `src/index.ts` imports
`@dagger.io/dagger`, which resolves to the generated `sdk/` directory, so a
target only reaches the module by being bound there — beside the module's own
types and its manifest dependencies, re-exported through `client.gen.ts`.
Without it, `dagger module client add` run inside a module produces a package
that module cannot use, which is the one place a user is most likely to run it.

The fold is filtered, not a merge. A target's schema is core plus that one
module, and its core half is the *client-facing* one, which hides nothing; the
module-facing schema deliberately withholds some core types. Folding it in whole
would hand a module bindings for types its own schema does not have. So each
client schema is passed through `Schema.Include` first, which keeps the target's
own types and, on the extendable types, only the fields it contributed
(`helpers/codegen/main.go`, `foldClientsIntoModuleSchema`).

One consequence for `Mod.generate`, which addresses a module by path: a scope's
client targets come from `dagger.toml`, which only the engine reads, so that
entry point cannot reconstruct them and regenerating would delete
`sdk/<target>.gen.ts`. It refuses when the module has a `clients/` directory
rather than silently pruning.

**The engine half is not there yet** (§8.5). The bindings type-check, but
nothing installs a client target into the module's own session, so
`dag.<target>()` compiles and then fails at run time unless the target is also a
manifest dependency. That is the engine's to close, not this SDK's — recorded
rather than worked around.

#### Naming and installability

The package is named for the **scope**, not for the directory it sits in: every
scope puts it in one called `clients`, so a name taken from that would be
identical in every scope and two of them could not be installed side by side.

It also carries a `version` (`0.0.0` when the user has not set one). npm and yarn
both refuse a package without one, so the `file:` dependency a user adds to reach
it would not resolve at all — a failure that lands on them, after generation
reported success.

Both land at the scope root. **Neither may go under `src/`**, and that is a hard
constraint rather than a preference: the introspector's `getTsSourceCodeFiles`
walks the source directory recursively and hands *every* `.ts` file it finds to
the AST as user source
(`library/src/module/entrypoint/introspection_entrypoint.ts:20`). The set of files
treated as generated is derived only from the `*.gen.ts` siblings of
`sdk/client.gen.ts` (`:45`), so bindings placed at `src/internal/clients/` would
be classified as module source — exactly the misclassification the
`generatedClientFiles` argument was added to prevent (`introspector/index.ts:14`).
Every generated client type would then be parsed as a candidate module API. It is
fixable, but only by changing the introspector *and* the committed bundle. Keeping
clients out of `src/` costs nothing and avoids the whole question.

The user installs the client-only package themselves — `"@dagger.io/<name>": "file:./.dagger/clients"`
plus their package manager. We generate a working package and stop there; we do
not edit their `package.json` to add the link.

The client-only case is the one that matters for the "don't touch user files"
rule. The scope was detected *because* it has a `package.json`, but that file is
the user's — we neither merge our dependencies into it nor add a `file:` link. The
generated package stands on its own under `.dagger/clients/`, with its own
`@dagger.io/dagger` pin and its own `node_modules` when installed, and the user
imports it however they prefer. `configureClientNode` already builds exactly this
shape; only its output location and its `existing` input change.

This needs one change in `helpers/codegen`: `generator.BoundModule`
(`helpers/codegen/generator/config.go:44`) and the `client.module` meta field
become a *list*, so one `serveBoundModule` bootstrap can serve several modules.
The generator already splits per-module files (`_dep.ts.gtpl`, `dep_split_test.go`),
so the templates mostly exist.

One detail this opens: `configureClientNode`'s `scopedClientName` derives the
package name from the single bound module (`@dagger.io/<module>-client`,
`helpers/config-updater/main.go:228`), which no longer holds for a package serving
several modules. **Decided: name it after the scope directory** —
`@dagger.io/<scope-basename>-client`, keeping the existing suffix and
sanitization. The helper already falls back to `@dagger.io/client` when
sanitization yields nothing (`:246`), which covers a scope at the workspace root.

Two scopes with the same basename (`apps/web`, `services/web`) produce the same
package name. That only collides if a project installs both, which is not a shape
the layout produces on its own — worth knowing, not worth designing around.

#### Deferred: moving the module bundle to `src/internal/`

The natural companion — `src/internal/clients` also holding the SDK bundle and
core bindings, `@dagger.io/<dep_name>` per dependency, and the entrypoint at
`src/internal/entrypoint` — is a better layout than today's `sdk/` +
`__dagger.entrypoint.ts`, but it **cannot ship with this migration**. Those paths
are the engine's builtin TypeScript runtime contract, not ours:

- `GenDir = "sdk"` and `EntrypointExecutableFile = "__dagger.entrypoint.ts"`
  (`sdk/typescript/runtime/main.go:28-31`).
- Call time mounts `<module>/sdk` **as** `node_modules/@dagger.io/dagger`
  (`runtime_node.go:180`, `runtime_bun.go:168`, `runtime_deno.go:149`), which is
  also why every dependency is re-exported through the single `@dagger.io/dagger`
  namespace rather than getting its own specifier.
- `requireGeneratedFiles` (`sdk/typescript/runtime/config.go:530`) hard-fails a
  committed-files module missing `sdk/client.gen.ts`, `__dagger.entrypoint.ts`
  or `tsconfig.json`.

`cli-1.0.md` non-goal 2 is "do not change the legacy runtime interface in this
work", so the move is a separate upstream change against `sdk/typescript/runtime`.

When it happens it also has to carry the introspector change: putting generated
bindings under `src/` needs `getTsSourceCodeFiles` to stop treating them as user
source, and `generatedClientFiles` to stop deriving its set from
`sdk/client.gen.ts`'s siblings. Two repos, one change — worth scoping together.

### 4.2 Paths stay workspace-root-relative

The obvious move, once `ws.cwd` is the scope, is to work from the cwd:
`ws.directory(".", include: ["package.json"])`, `ws.withDirectory(".", …)`, and
every `sourcePrefix`/`includePrefix` dance collapses. **That is not what the
implementation does, and the difference is not stylistic.** The engine resolves
a module's local `[[dependencies]]` to workspace-root-relative paths and then
reads them relative to `Workspace.cwd` (`ResolveDepToSource`) — so standing at
the scope's own cwd, a sibling dependency is looked for *underneath* the module
that declares it, and does not resolve.

So `generateScope` reads the cwd the engine set exactly once, through
`scopePath(ws)`, immediately roots the workspace with `ws.withWorkdir(".")`, and
addresses everything from the workspace root from there on
(`ws.directory("/", include: [scopeFile(scope, "package.json")])`,
`ws.withNewDirectory("/" + sourcePath, …)`). It restores the engine's cwd on the
way out, because the engine rejects a `generateScope` that returns a workspace
standing somewhere else.

One path convention, applied everywhere, is what makes that workable: `scopeFile`
joins a scope-relative filename onto the scope, and `existingDir`,
`existingModuleConfig`, `existingClientConfig`, `moduleSdkPath`, `moduleBindings`
and `clientBindings` all take root-relative paths. `ModConfig` keeps taking a
path for the same reason it always did — it is reachable from `Mod`, outside any
`generateScope` call.

### 4.3 Changeset framing disappears

`generateScope` returns a `Workspace`, and the engine diffs it
(`workspaceChangesBetween`, `core/schema/workspace_sdk_generator.go:466`) and
re-roots the changeset to the invocation cwd itself. Every comment in this repo
about changeset frames, `ws.changes(ws)` seeds, `withChangesets` versus
`withChanges`, and "one frame off for every cwd but the root"
(`typescript-sdk.dang:876-906`) becomes obsolete. This is a real simplification —
arguably the single best thing the migration buys us.

### 4.4 Deleting stale files

Changesets could only add, which is why `moduleFiles` layers onto a base with the
old `*.gen.ts` removed, and `existingClientBase` replaces rather than merges. With
a `Workspace` we can delete directly: `withoutDirectory` / `withoutFile`. The
demo Go SDK wipes and rewrites its whole client directory
(`modules/sdk-ux-go/main.dang:85`); with layout (b) we do the same for
`<scope>/.dagger/client/`, which makes `dagger module client rm` correct by
construction. `moduleBindings`' careful "which `*.gen.ts` do we own" filter stays
for the module half, where generated and user files share a directory.

### 4.5 The name comes from config, not from the module

`cli-1.0.md` is explicit: "The SDK module must use `name`. It must not infer the
scope name from `Workspace.cwd`." Today `moduleFiles` reads
`modSrc.moduleOriginalName`. Two knock-ons:

- `withName(name: name)` writes the name, so a manifest we create is consistent
  by construction.
- For an *existing* manifest we leave alone, `dagger.toml`'s `name` and the
  manifest's `name` can disagree. The engine does not reconcile them. We should
  generate from the argument and let the mismatch surface, rather than silently
  preferring one.

### 4.6 The manifest is ours to write

The builder is **not** an engine field. It was `moduleManifest` in core when this
document was written; the engine has since dropped it, and it now lives in
[`github.com/dagger/sdk-helpers`](https://github.com/dagger/sdk-helpers) — a
standalone Dang implementation declaring `engineVersion = "v0.21.9"`, so
depending on it does not itself require a new engine. It is this repo's only
module dependency.

`sdkHelpers.moduleManifest.withName(name:).withLegacyTypescriptRuntime(engineVersion:)`
plus `ws.withNewFile("dagger-module.toml", …)`, written when the scope has no
`dagger-module.toml`. `withEngineVersion` defaults to the running engine, so
`targetEngineVersion` is passed explicitly to keep the bundle/engine pairing this
repo enforces.

**Written when absent, not on every generation.** The manifest schema round-trips
through the builder, so rewriting one would preserve `source`, `include`,
`[[dependencies]]` and `[codegen]` — but this SDK records a scope's clients as a
generated package rather than as manifest dependencies (§4.8), so it has nothing
to reconcile into a manifest that already exists, and rewriting one would only
reformat what someone wrote by hand. The Python SDK does regenerate every time,
because it *does* write clients into `[[dependencies]]` and has to reconcile
them.

Writing once also used to be what kept generation a fixed point: the builder
spelled an absent module source out as `source = "."`, so a manifest written at
init came back changed on the next run. `dagger/sdk-helpers@64645f1` stopped
writing the default, and `generateMigratesLegacyConfigCheck` now asserts the
migrated manifest leaves it unwritten — so that hazard is the dependency's to
keep away, not an argument this decision still rests on.

**A pre-1.0 `dagger.json` is migrated**, which is the one case where a scope with
a module still needs a manifest written. The `dagger.json` is loaded, re-emitted
as TOML and then removed: leaving both would leave the module holding two
manifests free to disagree. Only a scope the workspace records is ever generated,
so migration reaches a module because someone asked for it, never in passing.

`source` handling: for a new module write nothing (defaults to `.`). For an
existing one, keep reading `ws.moduleSource(".").sourceSubpath` so a migrated
module whose `source` points elsewhere still gets its files in the right place —
but guard it, because `moduleSource(".")` fails when no manifest exists yet.

### 4.7 Runtime: from detection to setting

`ModConfig.runtime` infers NODE/BUN/DENO from `deno.json` / `bun.lock`. Module-max
says the SDK *selects* the runtime and may change it when scope settings change.
The two coexist cleanly:

- Explicit `runtime` scope setting → authoritative; we write the matching config
  files (`deno.json`, or `package.json` + `bun.lock`).
- No setting → detect from existing files, as today. Preserves every existing
  module.

This also removes a wart: `initModule` today takes `runtime` and writes an empty
`bun.lock` so the *engine* can detect BUN later. With the setting persisted in
`dagger.toml`, that file becomes a consequence rather than a signal — though the
engine's builtin runtime still detects from files at call time, so we keep writing it.

### 4.8 Module dependencies versus clients

Goal 3 of `cli-1.0.md` is "replace module dependencies with generated module
clients", and `dagger module deps` is removed. But `dagger-module.toml` still
parses `[[dependencies]]` (`core/modules/config.go`), and our module codegen still
emits one `<dep>.gen.ts` per dependency from `introspectionSchemaJSON`.

For this migration: **keep the dependency path working, add the client path.**
Existing modules with `[[dependencies]]` keep generating `sdk/<dep>.gen.ts`; new
cross-module wiring goes through `dagger module client add`. We do not write
dependencies into manifests we create — `clients` becomes a generated package,
not a manifest edit, and turning it into one would fight the design. That is also
what makes §4.6's "write the manifest once" safe: there is nothing about a scope's
clients that an existing manifest has to be told.

The ordering that `generateLocalDependencies` used to provide is now the engine's:
a scope whose clients target a local module scope depends on that scope, and the
engine generates leaf-first, threading the workspace through
(`core/schema/workspace_sdk_generator.go:432`). **This only covers client edges.**
A module-max scope whose *manifest dependency* points at another local scope has
no edge in that graph, so its dependency may not be generated first — the exact
problem `generateLocalDependencies` existed to solve. Worth confirming against the
engine before migrating `.dagger/modules/e2e/fixtures/generate-deps`, which is
built precisely on that case.

### 4.9 Legacy `dagger.json` modules leave our world

`Mod.generateStaged` routes `dagger.json` modules back to the engine
(`mod.dang:131`). Under module-max there is nowhere to route from: legacy modules
are not SDK scopes, `dagger generate` reaches them through the engine's own
module loading, and the legacy runtime regenerates at call time anyway. So that
branch — and the `isWorkspaceManaged` split, and `isTypescriptConfig`'s
`dagger.json` pattern in the *generation* path — can go.

The `dagger.json` fixtures in this repo (`generate/app`, `lookup/*`, `deps/*`,
`client/app`) stop being SDK-managed. They still matter for `Mod` discovery and
`ModConfig`, which is user-facing, so keep them there and drop them from the
scope list. A `dagger.json` scope someone *does* record is migrated rather than
refused — §4.6.

### 4.10 Skip marker

**Removed.** An earlier round of this design kept it, on the reasoning that
module-max supplies no replacement: the marker is found with `findUp`, so one
file at a repo root disables generation for every scope beneath it, which no
per-scope setting expresses; `[modules.typescript-sdk.generate.skip]` can only
disable the SDK wholesale, because the engine synthesizes a single
`typescript-sdk:generate` generator for all scopes
(`core/schema/workspace_sdk_generator.go:16`).

All true, and beside the point. Under module-max a module is generated **because
`dagger.toml` records it as a scope**, so "do not generate this tree" is already
spelled by leaving it out of the scope list. The marker's only remaining job is
to contradict a decision the workspace has already made, at a cost of one
`findUp` per scope per generate. The argument above was really an argument that
the scope list is the wrong granularity — which it is not, because the engine
never asks about anything else.

Un-managing a module and not generating it were different things when the SDK
discovered modules itself. They are the same thing now.

### 4.11 Idempotence is now a contract

"For the same Workspace, scope state, provider version, and effective settings,
the operation must return the same result." We already need this for
generate-as-checks; module-max makes it a stated requirement, and the engine
calls the same code path during `module init`, `client add`, `client rm`,
`client update`, `module update`, `workspace update` and `generate`. Any residual
"first run differs from second run" behavior — the `bun.lock` touch, template
rendering — has to be conditioned on the manifest marker, not on incidental state.

## 5. Migration plan

**Phase 0 — verify. Done.** All three assumptions held except the third:

1. `moduleSource(".")` resolves inside `generateScope` against a workspace we
   have just written the manifest into. ✔
2. `sourceRootSubpath` is the workspace-root-relative path the generated
   bootstrap needs. `asString` is a **host absolute** path for local sources
   (`core/modulesource.go:987`), so it cannot be used. ✔
3. Manifest-dependency ordering — **confirmed broken**, see §8.

**Phase 1 — the interface, module half.** Add `findClientRoot` and `generateScope`
handling `isModule` only, ignoring `clients`. Delete `initModule`, `initClient`,
the two `@generate` rollups, `modules(ws)`, `clientCwd`, `inCwdScope` and the
`generateLocalDependencies` staging. Have `moduleFiles` write the manifest,
keeping its paths workspace-root-relative (§4.2). At this point `dagger module
init typescript` and `dagger generate` work end to end for modules.

**Phase 2 — clients.** Extend `helpers/codegen`'s client meta to a list of bound
modules, emit the per-scope client package at the two locations from §4.1, and
handle removal by regeneration.

**Phase 3 — this repo's own config.** Rewrite `dagger.toml`: `[sdks.typescript] module = "typescript-sdk"`,
one `[sdks.typescript.scopes."<path>"]` per managed fixture with `is-module` and
`name`, `clients = [...]` for the client fixture. This is forced rather than
optional — any `dagger module` command rewrites the SDK sections and strips
`as-sdk` (`core/workspace/config_document.go:383`). Note that `name` is now
required in `dagger.toml` for every module scope
(`core/schema/workspace_sdk_generator.go:275`), so each fixture's name has to be
written down where it was previously read from the module.

**Phase 4 — tests.** See §6.

**Phase 5 — the pin.** `targetEngineVersion` and the committed bundle move
together to the first release carrying this PR.

The contract suite (`github.com/dagger/sdk-sdk`) is **retired here**, not
repinned. It drives a released CLI through the beta interface — `initModule`, the
`as-sdk` marker, the `@generate` rollups, `module deps` — every piece of which
this work removes, so its checks were reporting the removal rather than a
regression. `.dagger/modules/engine-e2e` covers what it covered, against the
branch that has the new interface (§6).

## 6. Test plan

`.dagger/modules/e2e` calls SDK functions directly, so the test surface tracks the
interface change one-to-one:

- `init.dang` (34 `initModule` calls) → `generateScope(isModule: true, …)` against
  a workspace whose cwd is the target scope. The assertions — merge-don't-replace,
  template selection, runtime config files, `packageManager`/`baseImage` — all
  survive; only the call shape and the argument source (settings, not arguments)
  change.
- `generate.dang` (8 `generateAllModule`) → `generateScope`. The
  dagger.json-stays-with-the-engine assertion (`generate.dang:249`) is deleted
  with §4.9.
- `client.dang` (10 `generateClient`, 2 `generateAllClient`) → `generateScope`
  with a populated `clients` list. Needs new cases: two clients in one scope, and
  removal by regeneration with one target dropped.
- `sdk.dang` → `targetRuntime` assertion becomes an assertion on the generated
  manifest's `[runtime] source`. The `skipGenerateFilename` assertion goes with
  the marker (§4.10).
- New: a `findClientRoot` check per marker file, plus the `null` case.
- New: idempotence — `generateScope` twice, second run empty
  (`generateIdempotentCheck`, on a dependency-free scope so it stays cheap).
- New: the client-only scope does not modify the user's `package.json` (§4.1) —
  the rule is easy to violate by reaching for `configUpdater` with the wrong
  `existing` directory.

Beyond that, the honest gap is that unit-testing a provider in dang does not prove
the engine accepts it. `sdkmodule.Implements` failing silently turns this SDK into
a plain installed module, so at least one check has to assert the shape the engine
validates, which means driving a real CLI.

**`.dagger/modules/engine-e2e` is that check.** It builds an engine from the
pinned dagger/dagger#13992 commit, runs it as a playground with this checkout
mounted, and drives the CLI through `dagger sdk list`, `dagger module init`
(default and with every setting), `dagger call`, and `dagger check` over the
`e-2-e:**` and `runtimes:**` groups. `devSdkCheck` asserts on the
`[sdks.typescript.scopes."…"]` block the engine writes, because a provider that
fails validation produces no error — the engine records nothing and this repo
becomes a plain installed module. `devSdkSettingsCheck` asserts each setting
reaches the generated files, which is §8.2.

The groups are named rather than left to a bare `dagger check`: `engine-e2e` is
one of the workspace's own modules, so an unfiltered run inside the playground
would build a second engine and recurse.

## 7. Decisions

All settled in review:

- **Client layout** — one generated package per scope, never per target; the
  user's own `package.json` is never touched (§4.1).
- **Module bundle layout** — `src/internal/clients` + `src/internal/entrypoint` is
  the layout we want, but it is blocked by the engine's builtin runtime and
  deferred to a separate upstream change (§4.1). `sdk/` and
  `__dagger.entrypoint.ts` stay for now.
- **`runtime` setting** — validated `String!` now, enum later (§3.4).
- **Skip marker** — removed. The scope list is what decides what is generated
  (§4.10).
- **The manifest** — built with `dagger/sdk-helpers`, not an engine field; written
  when the scope has none, not on every generation; a pre-1.0 `dagger.json` is
  migrated and removed (§4.6).
- **The released engine stays a target** — the module selects no engine field
  newer than `v1.0.0-beta.11`, so `[modules.e2e]` and `[modules.runtimes]` stay
  registered and their checks run in normal CI (§8.3).
- **Client package installation** — the user adds the `file:` dependency; we never
  edit their `package.json` (§4.1).
- **Clients never live under `src/`** — the introspector would classify them as
  module source (§4.1).
- **`defaultModulePath`** — not implemented (§3.2).
- **Legacy `dagger.json` modules** — an *unrecorded* one stays fully the engine's
  problem, and is dropped from this repo's scope list (§4.9). A scope the
  workspace does record is migrated to `dagger-module.toml` by this SDK, and its
  `dagger.json` removed (§4.6).
- **Manifest-dependency ordering** — expected to work; a gap is an engine bug, but
  verified in phase 0 because `generate-deps` is ours (§4.8).
- **Client package name** — derived from the scope directory,
  `@dagger.io/<scope-basename>-client`, plus a `version` so it is installable
  (§4.1).
- **A module scope's clients are bound twice** — the standalone package under
  `clients/`, and the module's own `sdk/<target>.gen.ts` so its source can reach
  them (§4.1).
- **`clients/` at a module scope root** — confirmed, beside the generated `sdk/`
  (§4.1).

Nothing in the design is open. What the implementation found in the engine is in
§8.

## 8. What the implementation found in the engine

8.1, 8.2 and 8.4 are gaps in `dagger/dagger#13992`, not here; 8.3 is not a gap at
all, but it decides how this repo runs its checks and is the most expensive thing
on this page to rediscover. 8.4 is the only one still blocking a user-facing
command.

### 8.1 Manifest dependencies have no edge in the scope graph

`planSDKModuleScopes` derives edges only from `scope.Clients`
(`core/schema/workspace_sdk_generator.go:334`). A module that depends on another
local module through its manifest's `[[dependencies]]` gets no edge, so scopes
generate in path order and a dependent can run first. The dependent's schema then
fails to load, because loading its dependency requires that dependency's
committed generated files:

```
generate SDK scope ".dagger/modules/e2e/fixtures/generate-deps/app":
  call SDK module generateScope: failed to load module dependencies:
  module "gendep" has runtime codegen disabled but committed generated file
  "sdk/client.gen.ts" is missing
```

This is reachable by any SDK whose modules still use manifest dependencies, which
is every module written before clients existed.

**Worked around here** by recording a client for `gendep` in the `generate-deps/app`
scope purely to create the edge (see the note in `dagger.toml`). The workaround
costs a generated client package the fixture does not otherwise need, and it only
works because we control the config — a user hitting this has no equivalent lever
short of adding a client they do not want.

### 8.2 SDK settings never reach the provider — fixed upstream

Scope and module settings are persisted correctly and handed to
`sdkmodule.Load`, but the constructed provider sees its declared defaults. With
`--runtime=bun --template=empty`:

```
Workspace.withInitModule(sdk: "typescript", ..., settings: "{\"runtime\":\"bun\",\"template\":\"empty\"}")
  → generateScope observes template="default" runtime=""
```

`[modules.typescript-sdk.settings]` fails the same way, so it is not scope-specific.
The same settings *do* apply on the normal call path (`dagger call typescript-sdk runtime`
prints the configured value) — but that path works because the CLI turns each
setting into an explicit constructor flag (`addSDKModuleSettingFlags`,
`internal/cmd/dagger/module_sdk_dynamic.go:182`). `Provider.instantiate`
(`core/sdkmodule/provider.go:207`) selects the constructor field with **no
arguments** and relies on `ApplyWorkspaceDefaultsToTypeDefs` having rewritten the
typedef defaults, which does not take effect for a Dang provider.

Not a stale-cache issue: `LegacyWorkspaceConfigJSON` is part of
`AsModuleVariantDigest` (`core/schema/modulesource.go:3525`). There is also no
engine test covering settings reaching a provider — `workspace_sdk_cli_test.go`
only asserts config listing.

**Fixed on the branch.** The description above is what `2dfc08f7` did; at
`8fd9b22b` the settings arrive. `engine-e-2-e:dev-sdk-settings-check` pins that
down from this side — one assertion per setting, on the file it changes, so a
regression shows up as a failing check rather than as a flag that quietly does
nothing. The e2e checks still construct the SDK with explicit arguments, which is
what the engine does; the two halves are verified separately.

### 8.3 One missing engine field fails every check that touches the SDK

Not a defect, but the constraint that decides how this repo runs CI. Dang infers
a whole program on each call, so the moment the SDK module selects an engine
field the running engine does not have, *every* call into that module fails —
including a check that only reads a constant `String!` field. Selection cannot
route around it, and check selection cannot either.

That is what produced 67 red checks on the first round of this work: the module
selected `moduleManifest` and `Workspace.withFile`, neither of which exists in
`v1.0.0-beta.11`, so `e-2-e:*` and `runtimes:*` failed wholesale on the engine CI
runs on. It reads as a broken SDK and is really one unavailable field.

It is avoidable here, and avoiding it is worth more than it costs:

- `moduleManifest` moved out of the engine and into a module dependency (§4.6),
  which removes it from the question entirely.
- `Workspace.withFile` has a `withNewFile` equivalent that predates it, at the
  cost of reading the manifest's contents rather than passing a `File`.

With those two, the module selects nothing newer than `v1.0.0-beta.11`, so
`[modules.e2e]` and `[modules.runtimes]` stay registered and the whole suite runs
in normal CI — while `engine-e2e` runs it again on the branch engine, which is the
one this work targets. Losing that property is a real cost, not a formality: the
alternative is unregistering the modules so the released engine never loads them,
and then the only signal for the entire SDK is one nested-engine check.

New engine fields will eventually be worth taking. The point is to notice when
one is being spent.

### 8.4 A non-null `findClientRoot` cannot be read back, so `client add` is broken

`dagger module client add` fails for **every** SDK provider whose
`findClientRoot` answers with a path:

```
call SDK module findClientRoot: assign: Setter.SetField dagql.Result[dagql.Typed]
  to dagql.Nullable[dagql.String]: assign: Setter.SetField dagql.DynamicOptional
  to dagql.Nullable[dagql.String]: dynamic optional: assign: Setter.SetField
  dagql.String to dagql.Nullable[dagql.String]: cannot set field of type
  dagql.Nullable[dagql.String] with dagql.String
```

Nothing about the value is wrong. `Provider.FindClientRoot` selects into a
`dagql.Nullable[dagql.String]` (`core/sdkmodule/provider.go`), the module returns
a well-formed `DynamicOptional` wrapping a `String`, and the failure is entirely
in reading it back:

- `DynamicOptional.SetField` sees a destination whose `reflect.Kind` is `Struct`,
  takes its `default` branch, and assigns the **unwrapped** value —
  `assign(val, o.Value)` (`dagql/nullables.go`).
- `assign` finds `dagql.String` is not assignable to `dagql.Nullable[dagql.String]`
  and falls through to `String.SetField`, which only accepts a `string`
  destination (`dagql/objects.go`, `dagql/types.go`).
- `Nullable[T]` is `Typed` and `Derefable` but not a `Setter`, and nothing in
  `assign` constructs a `Nullable[T]` from a `T`. There is no path that succeeds.

`Valid == false` returns early and never assigns, so the **null** answer works.
That is the whole reason this went unnoticed: `module init` never calls
`findClientRoot` (`cli-1.0.md` §findClientRoot table), so the only command that
reaches it is `client add`, and the only answer that had been exercised is the
one that declines.

The fix is upstream, in `Optional.SetField` and `DynamicOptional.SetField`:
recognize a `Nullable[T]` destination and set its `Value`/`Valid` rather than
assigning the unwrapped value into it. Nothing in this repo can work around it —
the interface fixes the return type, and the engine never inspects what we
returned beyond unwrapping it.

Reproduced at the pinned `8fd9b22b` with two plain TypeScript modules:
`dagger module init typescript` twice, then `dagger module client add typescript`
from inside one of them. `.dagger/modules/engine-e2e` should grow a check for it
— `client add` is the only command that covers this half of the interface, and
its absence is what let this reach a user.

### 8.5 A module's client targets are not served into its own session

`dagger module client add` inside a module records the target and generates both
halves of it — the standalone package under `clients/`, and the module's own
`sdk/<target>.gen.ts` (§4.1) — and the module's source then type-checks against
it. At run time the call fails:

```
Cannot query field "scratchtarget" on type "Query". Did you mean "scratchhost"?
```

A module's session installs what its manifest lists in `[[dependencies]]`. Scope
clients live in `dagger.toml` and are read only to build the generation graph and
to hand `generateScope` its targets (`resolveSDKModuleScopeClients`); nothing
adds them to the module the engine then runs. So the one thing `client add`
inside a module is for — calling that module from this one — is the thing that
does not work.

Nothing here can close it. The SDK is not told which targets a scope records at
run time, only at generation time, and the module's dependency set is the
engine's. The alternative, having the generated entrypoint serve each target
before dispatch, puts a workaround for engine-owned state into every generated
module.

Reproduced at `66da6410` with two modules created by `dagger module init
typescript`, `dagger module client add typescript ../<target>` from inside one of
them, a `dag.<target>()` call in its source, and `dagger call` on the result.
`.dagger/modules/engine-e2e` should grow a check for it once the engine serves
them, alongside the `client add` one §8.4 already asks for.

## 9. What was verified

Against the released `v1.0.0-beta.11` engine, which is where CI runs:

- The whole `e-2-e:*` and `runtimes:*` suite, plus the `helpers/*` Go tests.
  Nothing in the module selects a field that engine does not have (§8.3).

Against a dev engine built from the pinned `8fd9b22b`, through
`.dagger/modules/engine-e2e` rather than by hand — so it is a check that keeps
running, not a note about one afternoon:

- `dagger sdk list` — the interface passes the engine's exact-shape validation.
- `dagger module init typescript` — seeds a module, records the scope in
  `dagger.toml`, writes a manifest and no `dagger.json`, and `dagger call` on the
  result returns a value, so the generated artifact really runs.
- `dagger module init typescript --template/--package-manager/--base-image/--runtime`
  — every setting reaches the files it should (§8.2).
- The `e-2-e:**` and `runtimes:**` groups again, inside that engine.

Idempotence (§4.11) is asserted directly, by `generateIdempotentCheck` in
`e-2-e:generate`: generate a scope, apply the changeset, generate again, and the
second changeset must be empty. An earlier round had also verified it by hand —
`dagger generate typescript-sdk:generate` against `2dfc08f7`, all nine scopes
generated including the dependency-ordered pair and the client-only scope, a
second run reporting "no changes to apply" — which the check now covers on every
run.
