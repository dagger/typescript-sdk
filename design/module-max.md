# Adopt the module-max SDK interface

Status: implemented and verified against a dev engine at `2dfc08f7`. Tracks [dagger/dagger#13992](https://github.com/dagger/dagger/pull/13992)
("Better SDK UX: modules-max design"), whose source of truth is
[`future/cli-1.0.md`](https://github.com/dagger/dagger/blob/sdk-ux-module-max/future/cli-1.0.md)
on branch `sdk-ux-module-max`. Engine line references below are from that branch.

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
detectScope(ws: Workspace!): String!
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

### 3.1 `detectScope`

Answers "which directory is the TypeScript project containing `ws.cwd`". Used
only for **clients** (`module init` never calls it — `cli-1.0.md` §detectScope
table). Must return a workspace-root-relative parent of `ws.cwd`, or `""`
(`core/schema/workspace_sdk_module.go:828`).

```dang
detectScope(ws: Workspace!): String! {
  let found = scopeMarkers.reduce(null) { acc, name =>
    let hit = ws.findUp(name)
    if (configHitDepth(hit) > configHitDepth(acc)) { hit } else { acc }
  }
  if (found == null) { "" } else { configDir(found) }
}
```

with `scopeMarkers = ["package.json", "deno.json", "deno.jsonc", "tsconfig.json"]`.
Deepest hit wins, reusing the `configHitDepth` ranking already in
`typescript-sdk.dang:144`.

Two consequences worth stating up front:

- **You cannot create the first TypeScript client scope in a directory with no
  TypeScript project.** `detectScope` returns `""`, and `dagger module client add`
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
  if (skipGenerate(ws)) {
    ws
  } else {
    let withModule = if (isModule) { moduleFiles(ws, name) } else { ws }
    clientPackage(withModule, clients)
  }
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
  `detectScope` returns the directory holding `package.json`, so nothing we
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
  runtime: String! = "node"        # parsed into the internal Runtime enum
  template: String! = "default"
  packageManager: String! = ""
  baseImage: String! = ""
  # existing: targetEngineVersion
  # private (let): skipGenerateFilename, moduleConfigFilenames, scopeMarkers
}
```

giving `dagger module init typescript --runtime=bun --template=empty` and
`dagger module client add foo --typescript-runtime=bun`, persisted into
`[sdks.typescript.scopes."<path>".settings]`.

`cli-1.0.md` uses this SDK as its example of a settings-driven provider:

```text
typescript-sdk  runtime  node  JavaScript runtime for generated code
```

Only knobs a user should turn are settings. `skipGenerateFilename` and the
config-filename lists become private `let` bindings — they are internal
constants, and as settings they would show up as noise in `dagger module settings`
and as flags on `module init`.

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
| Module (`is-module = true`) | `clients/`, beside the generated `sdk/` | none — the module's own `package.json`/`tsconfig.json` are already ours to generate, so clients are aliased into them |
| Client-only | `.dagger/clients/` | self-contained: its own `package.json` and `tsconfig.json`, generated |

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

### 4.2 Everything becomes cwd-relative

`typescript-sdk.dang` reads and writes through workspace-root-relative paths
(`ws.directory("/", include: [prefix + "package.json"])`, `ws.withNewDirectory("/" + sourcePath, …)`)
because `currentModule.asSDK` handed back root-relative module paths. With
`ws.cwd` set to the scope, all of that becomes `ws.directory(".", include: ["package.json"])`
and `ws.withDirectory(".", …)`. `existingDir`, `existingModuleConfig`,
`existingClientConfig`, `moduleSdkPath`, `moduleBindings`, `clientBindings` and
every `sourcePrefix`/`includePrefix` dance collapse. `ModConfig`'s `sourceFile`
prefixing goes the same way — though `ModConfig` is also reachable from `Mod`
outside a `generateScope` call, so it keeps taking a path and only the
`generateScope` caller passes `.`.

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

- `moduleManifest.v1(name: name)` writes the name, so a manifest we create is
  consistent by construction.
- For an *existing* manifest we leave alone, `dagger.toml`'s `name` and the
  manifest's `name` can disagree. The engine does not reconcile them. We should
  generate from the argument and let the mismatch surface, rather than silently
  preferring one.

### 4.6 The manifest is ours to write

`moduleManifest.v1(name:).withRuntime(source: "typescript").asFile` +
`ws.withFile("dagger-module.toml", …)`, only when the file is absent — the marker
that distinguishes init from regeneration (`cli-1.0.md` §generateScope). The
builder also has `withSource`, `withEngineVersion` and `withInclude`
(`core/module_manifest.go`); `withEngineVersion` defaults to the running engine,
so `targetEngineVersion` should be passed explicitly to keep the bundle/engine
pairing this repo enforces.

The builder cannot write `[[dependencies]]` — see §4.8.

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
dependencies into manifests we create — the manifest builder cannot, and doing it
by hand would fight the design.

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
`skip/app`, `client/app`) stop being SDK-managed. They still matter for `Mod`
discovery and `ModConfig`, which is user-facing, so keep them there and drop them
from the scope list.

### 4.10 Skip marker

**Kept as-is.** `generateScope` returns `ws` unchanged when
`findUp(skipGenerateFilename)` hits, at a cost of one `findUp` per scope per
generate. Only the filename moves from a public setting to a private constant
(§3.4) — the mechanism does not change.

Module-max does not supply a replacement, despite appearances:

- A per-scope `skipGenerate` setting would be strictly *less* useful. The marker
  is found with `findUp`, so one file at a repo root disables generation for
  every scope beneath it; a setting has to be repeated per scope entry.
- `[modules.typescript-sdk.generate.skip]` looks like the right tool but the
  engine synthesizes a single `typescript-sdk:generate` generator for **all**
  scopes (`core/schema/workspace_sdk_generator.go:16`), so that key can only
  disable the SDK wholesale.
- Deleting the scope from `dagger.toml` stops generation but also un-manages the
  module, which is a different thing.

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

**Phase 1 — the interface, module half.** Add `detectScope` and `generateScope`
handling `isModule` only, ignoring `clients`. Delete `initModule`, `initClient`,
the two `@generate` rollups, `modules(ws)`, `clientCwd`, `inCwdScope` and the
`generateLocalDependencies` staging. Convert `moduleFiles` to cwd-relative and
have it write the manifest. At this point `dagger module init typescript` and
`dagger generate` work end to end for modules.

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

**Phase 5 — the pin.** `targetEngineVersion`, the committed bundle, and
`[modules.sdk-sdk].settings.daggerCliVersion` all move together to the first
release carrying this PR. The contract suite (`github.com/dagger/sdk-sdk`) drives
a real CLI through `dagger module init` / `client add`, so it needs its own
module-max update before it can gate us — that is an upstream dependency, not
something this repo can land alone.

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
  manifest's `[runtime] source`. The `skipGenerateFilename` assertion cannot stay
  as written once the field is private (§3.4): it becomes a behavioral check —
  drop the marker in a scope, assert `generateScope` returns no changes.
- New: a `detectScope` check per marker file, plus the `""` case.
- New: idempotence — `generateScope` twice, second run empty.
- New: the client-only scope does not modify the user's `package.json` (§4.1) —
  the rule is easy to violate by reaching for `configUpdater` with the wrong
  `existing` directory.

Beyond that, the honest gap is that unit-testing a provider in dang does not prove
the engine accepts it. `sdkmodule.Implements` failing silently turns this SDK into
a plain installed module, so at least one check should assert the shape the engine
validates — realistically by driving a real CLI, which is the contract suite's job.

## 7. Decisions

All settled in review:

- **Client layout** — one generated package per scope, never per target; the
  user's own `package.json` is never touched (§4.1).
- **Module bundle layout** — `src/internal/clients` + `src/internal/entrypoint` is
  the layout we want, but it is blocked by the engine's builtin runtime and
  deferred to a separate upstream change (§4.1). `sdk/` and
  `__dagger.entrypoint.ts` stay for now.
- **`runtime` setting** — validated `String!` now, enum later (§3.4).
- **Skip marker** — kept exactly as it works today; only the filename becomes a
  private constant. No scope setting replaces it (§4.10).
- **Client package installation** — the user adds the `file:` dependency; we never
  edit their `package.json` (§4.1).
- **Clients never live under `src/`** — the introspector would classify them as
  module source (§4.1).
- **`defaultModulePath`** — not implemented (§3.2).
- **Legacy `dagger.json` modules** — fully the engine's problem; dropped from our
  scope list (§4.9).
- **Manifest-dependency ordering** — expected to work; a gap is an engine bug, but
  verified in phase 0 because `generate-deps` is ours (§4.8).
- **Client package name** — derived from the scope directory,
  `@dagger.io/<scope-basename>-client` (§4.1).
- **`clients/` at a module scope root** — confirmed, beside the generated `sdk/`
  (§4.1).

Nothing in the design is open. What the implementation found in the engine is in
§8.

## 8. Engine gaps found during implementation

Both are in `dagger/dagger#13992`, not here, and both are worth reporting
upstream before that PR merges.

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

### 8.2 SDK settings never reach the provider

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

**Not worked around.** Every SDK setting this repo exposes — `runtime`,
`template`, `packageManager`, `baseImage` — is silently ignored by
`dagger module init` and `dagger module client add` until this is fixed. The
settings are persisted, so they start working the moment it is. The e2e checks
cover the behaviour by constructing the SDK with explicit arguments, which is
what the engine is supposed to do, so our half is verified independently.

## 9. What was verified

Against a dev engine built from `2dfc08f7` (`dagger-dev`, runner
`docker-container://dagger-engine.dev`):

- `dagger sdk list` / `dagger sdk scope list` — the interface passes the engine's
  exact-shape validation and every scope is registered.
- `dagger generate typescript-sdk:generate` — all nine scopes generate, including
  the dependency-ordered pair and the client-only scope; a second run reports
  "no changes to apply" (§4.11 idempotence).
- `dagger module init typescript --name=… --path=…` — seeds a module, and
  `dagger call` on the result returns a value, so the generated artifact really
  runs.
- `dagger module init typescript --help` — all four settings register as flags
  (they just do not take effect yet, §8.2).
- 41 e2e checks and the `helpers/codegen` Go tests.
