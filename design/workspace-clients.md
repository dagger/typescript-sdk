# Workspace clients: one shared `.dagger/clients` for everything

> **Status: implemented, verified end to end against a live engine.** Successor
> to the last open item of [`unified-clients.md`](./unified-clients.md): the
> per-module client *shape* converged there ("what remains is merging the two
> directories, not the two shapes"), and this merges the directories. Grounded
> against the `manifest-v2` engine branch (dagger/dagger#14038, `3f729ec933`).
>
> **Verification.** On a repro workspace with an entrypoint module (`test`, a
> git dependency `hello`, a self client) and a client-only scope (`my-code`):
> `dagger generate` writes one `.dagger/clients` and prunes both old locations;
> regeneration is byte-quiet, including from either subdirectory;
> `dagger api call test self-call` and `test remote-dep` run; `npm install` +
> `tsx main.ts` in `my-code` reaches the module and its git dependency; `tsc`
> is clean for both the consumer and the module. The 49-check e2e suite and the
> three runtime-execution checks pass. Two assertions in that suite were
> already failing on the base branch and are fixed here (§8).

## 1. Summary

A workspace's generated clients all live in **one directory beside
`dagger.toml`**:

```
.dagger/clients/
  dagger/        the vendored @dagger.io/dagger library package
  <module>/      one @dagger.io/<module> package per module used anywhere
```

One copy per module for the whole workspace, whoever consumes it: a module's
own source, another module, or plain user code in a client-only scope. The
package layout is the one the standalone client scope already renders — the
library as a real package, one package per module with `file:` links — chosen
because it is consumable through plain npm resolution and forward-compatible
with publishing.

What makes this possible without deduplication machinery is that the renderings
already converged: a module client file is **byte-identical** whether it was
generated for a module scope or a client-only scope (verified on the manual
repro — `test.gen.ts`, `index.ts`, `core.js` identical across the two
locations). One render, one location, no dedup — there is nothing left to
deduplicate.

## 2. What moves, what stays

| | before (unified-clients branch) | after |
| --- | --- | --- |
| client-only scope | `<scope>/.dagger/clients/{dagger,<m>}/` | `/.dagger/clients/{dagger,<m>}/` (workspace root) |
| module: library | `<mod>/sdk/` | `/.dagger/clients/dagger/` |
| module: clients | `<mod>/clients/<m>.gen.ts` | `/.dagger/clients/<m>/<m>.gen.ts` |
| module: loader | `<mod>/clients/loader.gen.ts` | `<mod>/loader.gen.ts` (module root — it is dispatch machinery, not a client) |
| module: dispatcher | `<mod>/__dagger.dispatch.ts` | unchanged |
| module: entrypoint | `<mod>/entrypoint/main.dang` | unchanged (recipe paths change) |
| tsconfig aliases | `./sdk/index.ts`, `./clients/<m>.gen.ts` | `<rel>/.dagger/clients/dagger/index.ts`, `<rel>/.dagger/clients/<m>/<m>.gen.ts` |
| consumption (user code) | `npm install ./.dagger/clients/<m>` | `npm install <rel>/.dagger/clients/<m>` |

`dagger.toml` is untouched: scopes keep recording *who consumes what*
(`clients = [...]` per scope) — that stays the consumer declaration and the
engine keeps resolving it. Only the *materialization* is shared.

### 2.1 Which modules get the shared layout

**Gated on the `entrypoint` setting.** The engine's built-in TypeScript runtime
(`[runtime] source = "typescript"`) mounts only the module's own directory into
the runtime container, so a legacy module cannot reach files at the workspace
root at all — no `include` can fix that (the runtime mounts
`ContextDirectory().Directory(subPath)`, not the context), and a v2 manifest
rejects `include` outright anyway (`config_format.go:230`). So:

- `entrypoint = true` module scopes: shared layout, no `sdk/`, no `clients/`.
- legacy module scopes: today's embedded layout, unchanged.
- client-only scopes: shared layout always — nothing runs them in the engine,
  so there is no runtime constraint.

## 3. How a module reaches clients outside its directory

This was the open question ("`[[include]]` or copy via host?"), and the answer
is **neither — the recipe already mounts the workspace.** The generated
entrypoint's `call()` receives the caller's `currentWorkspace`
(`core/sdk/dang/v2/entrypoint.go:196`) and the baked recipe already does
`withMountedDirectory("/workspace", workspace.directory("/"))`. The shared
directory is *in that mount*. Two path-level changes complete it:

- **Runtime resolution (node/bun):** mount
  `workspace.directory("/.dagger/clients")` at `node_modules/@dagger.io` —
  the directory names map 1:1 onto the `@dagger.io/*` package names, so one
  mount replaces today's `sdk/ → node_modules/@dagger.io/dagger`. A package
  importing a sibling (`@dagger.io/test` → `@dagger.io/dagger`) resolves by
  ordinary upward node_modules lookup; the `file:` links in package.json are
  npm-install wiring for host-side consumers and inert here. Deno keeps
  resolving through its import map, whose entries just point at the new
  relative locations inside the `/workspace` mount.
- **Editor resolution:** tsconfig `paths` point at the relative location
  (`../../clients/dagger/index.ts` from `.dagger/modules/<m>`).

`[[include]]` was rejected for the reason above; a `host` copy is not even
expressible (an entrypoint has no `host`; the injected `workspace` argument is
its only filesystem handle, and it already suffices).

## 4. Who writes the shared directory

The engine drives generation per scope (`workspace_sdk_generator.go:394`):
scopes fold sequentially over one threaded workspace, modules ordered before
scopes holding clients for them, and — decisive for us — **the returned
workspace is diffed at the workspace root**, not the scope
(`workspace_sdk_module.go:1109` → `workspaceChangesBetween`, root-to-root; the
CLI previews with `WithWorkdir(".")` for exactly this). Writing above the scope
is supported and integration-tested (`TestSDKModuleCanWriteAboveScope`).

So: **every scope's `generateScope` renders the full union and writes it**,
with the file-level `writeChanged` skip keeping it byte-quiet. Because the
render is deterministic in the workspace state, every scope writes the same
bytes: the first pass in engine order materializes changes, later passes
compare-and-skip. Removal falls out: the union is the complete desired state
of the directory, so a target no scope records any more is pruned by whichever
scope generates next.

The union cannot come from accumulating the per-scope `clients` arguments: a
`dagger generate` run from a subdirectory only plans scopes containing or
inside the cwd (`sdkGenerationScopeApplies`), and a union built from planned
scopes alone would prune the siblings' packages. Instead the SDK reads
`dagger.toml` itself (readable; only *writing* it is engine-guarded) through a
small Go helper, and takes:

- every recorded scope's `clients` list, for scopes that consume the shared
  directory (client-only scopes, entrypoint module scopes),
- plus the self package of every entrypoint module scope,

resolving each ref with `ws.moduleSource("/<path>")` / `ws.moduleSource(ref)`
(root-anchored, lock-pinned). Scopes of other SDKs are ignored; ours are found
by matching `[sdks.<k>] module` against `currentModule`'s name. A pure-legacy
workspace has no consumers, so no shared directory appears.

**Conflicts become errors.** One directory means one version per module name
workspace-wide: two scopes binding the same name to different sources
(ref+pin / path) is raised with both scopes named, rather than last-writer-wins
churn. This is the point of a single source of truth, and the cost of it.

**Freshness follows engine order.** A module's package renders from its
client-facing schema, read from the threaded workspace. A scope generated
before some unrelated module M may render M's package from M's pre-regeneration
schema, but M's own pass (and every pass after it) re-renders it fresh, so the
final state is fresh regardless of order; intermediate rewrites are only
changeset noise. Schema reads are content-addressed on the module's subtree, so
cross-pass cache hits collapse the N-scopes-times-M-targets cost.

## 5. The one shared core

A module's `client.gen.ts` was rendered from the *module-facing* schema, a
client scope's from the *client-facing* schema. Verified on the repro: the
client-facing render is a **strict superset** (adds `Host`, `Engine`,
`currentWorkspace`, …; zero deletions). The shared `dagger/` package uses the
client-facing render — that is what the union render (`codegen client`)
already produces.

Trade-off, accepted: module code can now *type-check* against client-only API
(`dag.host()`) that still fails at runtime in a module session. The compile
error becomes a runtime error. The alternative — two core packages — is the
duplication this design exists to remove.

### 5.1 Core comes from the newest target, not the first

Found while verifying, and it would have shipped as a runtime error: the engine
serves each module a **compatibility view** of core keyed on the engine version
that module declares. The repro's `hello`, pinned at `hello/v0.3.0`, reports
`__schemaVersion: v0.12.0` and a `Container.withDirectory(directory:)` where
the current engine takes `source:`. `mergeSchemas` took core from the *first*
schema, so with `hello` sorting first the whole workspace rendered against a
four-releases-old core and every query it sent was rejected —
`Unknown argument "directory" on field "Container.withDirectory"`.

`codegen client` now picks the newest-versioned target's schema as the base and
takes only each other target's own contribution (`Include(DependencyNames()…)`),
which is what module mode already did with the module's own schema as base. A
module pinned old still gets bindings generated from the view it is served
under; only core is shared, and it is current.

This was latent before — a client-only scope with an old git target first would
already have hit it — but the shared directory merges every module in the
workspace into one render, so it becomes the common case.

### 5.2 Packages reach each other by path, not by name

Also found while verifying. npm links a `file:` dependency as a **symlink**, and
node resolves from the **real path**: a client installed into a consumer's
`node_modules` looks for `@dagger.io/dagger` beside its own directory in the
shared tree — which has no `node_modules` — and fails. The old layout hid this
because the packages sat under the consuming scope, so the walk up from the real
path still reached the consumer's `node_modules`.

So inside the shared directory the generated files import by relative path
(`../dagger/index.js`, `../hello/hello.gen.js`). The `package.json` names and
the specifier a *user* writes are unchanged — `@dagger.io/<module>` is still the
public surface, resolved by npm for a consumer and by the tsconfig alias for a
module's source. Only the edges between generated files stop depending on npm
topology, which also makes the directory work under the runtime's single
`node_modules/@dagger.io` mount without any install at all. Publishing one
package per module later means emitting bare specifiers again — one knob,
`ModuleGeneratorConfig.PackagedClients`.

## 6. Generation mechanics per scope kind

**Client-only scope:** render the union, write the shared directory, prune the
scope-local `.dagger/clients` left by the previous layout. Nothing else — the
user's own config is never touched; they wire packages in with
`npm install <rel>/.dagger/clients/<m>`.

**Entrypoint module scope:** unchanged two-pass structure, minus the embedded
copies:

1. `codegen module` still runs (both passes): its bindings feed the typedef
   scan staging (unchanged shape, throwaway) and its `loader.gen.ts` — the only
   binding still written into the module, at the module root; the dispatcher's
   import moves to `./loader.gen.js`.
2. `sdk/` and `clients/` are no longer written, and are pruned when present
   (the flip from embedded to shared, like the legacy-entrypoint prune).
3. Config aliases point at the shared directory; the dang recipe mounts it at
   `node_modules/@dagger.io`; `requireGenerated` checks split between module
   files (dispatcher, loader, tsconfig) and shared files
   (`.dagger/clients/dagger/…`).
4. The shared directory is rendered and written last, from the union, against
   the workspace state that already includes this module's fresh files — so
   its own package (self client) is fresh in its own pass.

**Legacy module scope:** untouched — embedded `sdk/` + `clients/`, today's
aliases, today's dispatcher import.

## 7. Testing

The e2e client checks move with the layout: `clientDir` is now one constant, not
a function of the scope. The fixtures share this repo's workspace and therefore
its shared directory, which is only safe because each check computes a
`Changeset` against the same untouched base and never applies it — two checks
generating different targets never see each other's writes. Module-scope checks
are unaffected: the harness leaves `entrypoint` off, so they still exercise the
embedded layout, which is exactly what legacy modules keep (§2.1).

Coverage added: the packaged import layout (`dep_split_test.go`), the
newest-core merge and version ordering (`main_test.go`), the shared-clients
recipe and generated-file guard (`dang_entrypoint_test.go`), the relocated
loader import (`entrypoint_test.go`), the packaged alias targets
(`config-updater`), and the `dagger.toml` reader (`workspace-config`).

## 8. Two assertions that were already failing

Found while running the suite; both fail on the base branch and are fixed here,
because they assert against a shape the unified-clients work already changed:

- `generateClientCheck` / `multipleClientsCheck` asserted `asModule().serve()`.
  That is the *git* serve; every fixture target is local and serves through a
  raw `currentWorkspace` query. They now assert the hook both kinds attach,
  `withServe({ key: "<module>"` .
- `generateSelfCallCheck` asserted `export const dag = new Client()` and that
  the *dispatcher* carries `asModule { serve }`. Serve-on-use moved the serve
  onto each client, so the self client's `dag` now takes a serve spec and the
  dispatcher serves nothing. Both assertions now read the self client.

## 9. Open questions / follow-ups

- **Git consumption of entrypoint modules** stays the open item it already was
  (`module-entrypoint.md` §10.3): the entrypoint receives the *consumer's*
  workspace, which contains neither the module nor — now — its clients. The
  engine clones the whole repo but filters the context to module dir + config
  (v2: no includes). The eventual fix is orthogonal (recipe falling back to
  `currentModule.source`, which *is* reachable and correct in the entrypoint,
  plus either engine-side workspace-aware git loading or published packages
  replacing `file:` links). This design neither helps nor hurts it.
- **Engine bug found while grounding this** (to report upstream): a v2 module
  whose source root is `.` degenerates its context include list to the manifest
  only (`SourceSubpath` stays `""`, skipping the `"." → "*"` conversion in
  `loadModuleSourceContext`), so a root-level entrypoint module cannot find its
  `entrypoint/` dir when loaded as a source.
- **`dagger.toml` below the git root:** everything here anchors at the
  directory holding `dagger.toml` (found by `findUp`), not the workspace root,
  matching where `.dagger/modules` lives.
- **The `typescript` pin** in generated package.json is still to drop
  (`module-entrypoint.md` §8.2), independent of this.
