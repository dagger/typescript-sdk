# Embed the TypeScript runtime into generated modules

> **Status: proposal.** Successor to [`runtime-module.md`](./runtime-module.md),
> which proposed moving the runtime here as a separate Dang module addressed by
> git ref. The engine has since gained a strictly better delivery mechanism —
> `embed:<filename>` — so this replaces that proposal's §7 (layout/delivery) and
> §10 (rollout) while keeping its §6 (the `typescript` problem) and §8
> (optimizations) intact.
>
> Depends on engine PR [dagger/dagger#14018](https://github.com/dagger/dagger/pull/14018)
> (open, `v1.0.0-beta.12`). Reference implementation:
> [dagger/python-sdk#24](https://github.com/dagger/python-sdk/pull/24).

## 1. Summary

`dagger generate` emits a single self-contained Dang file, `runtime.dang`, next
to the module's `dagger-module.toml`, and modules record:

```toml
[runtime]
  source = "embed:runtime.dang"
```

An embed-aware engine reads that file out of the module's own directory and
evaluates it in-process with its native Dang interpreter. No SDK module is
resolved, fetched, compiled or loaded.

The design decision that shapes everything below: **the emitted runtime is
specialized at generation time.** We know, when we generate, which JavaScript
runtime the module uses, which package manager, which base image, and whether it
has any dependencies at all — so we emit the runtime for *that* module rather
than a universal one that re-derives all of it on every call. What lands in the
module is a straight-line container recipe with the answers already baked in:

```
runtime/node/main.dang ─┐
runtime/bun/main.dang  ─┼─ generation picks one, splices the config ─▶ <module>/runtime.dang
runtime/deno/main.dang ─┘
```

The result does one thing: **build a container that installs (if needed) and
runs the committed entrypoint.** No codegen, no introspection, no config
parsing, no branching on things that were already known.

## 2. The contract

Enforced by the engine, verbatim (`core/sdk/dang/v2/embedded_runtime.go:225`):

```dang
pub moduleRuntime(modSource: ModuleSource!, introspectionJson: File): Container!
```

| Rule | Where | Consequence for us |
| --- | --- | --- |
| Ref is `embed:<bare .dang filename>` | `core/sdk/embedded_runtime.go:34` | no path, no metacharacters; always `runtime.dang`, whichever of the three we emit |
| File read from the module's **source root** (dir holding `dagger-module.toml`) | `core/sdk/embedded_runtime.go:181` | generation writes it at `rootPath`, not `sourcePath` (§7.3) |
| Auto-included in the module context, after user patterns | `core/schema/modulesource.go:1230` | no `include` entry needed; a negation cannot undo it |
| Evaluated alone in a temp dir (`dang.RunDir`) | `core/sdk/dang/v2/embedded_runtime.go:174` | **one file** — no imports of sibling `.dang` files |
| Exactly one type may declare `pub moduleRuntime` | `:189` | helper types allowed, but we should not need any |
| That type must be constructible with no arguments | `:225` | every field carries a default |
| `introspectionJson` declared, nullable, **never passed** | `:225`, `:26` | declare it, ignore it — §4 |
| Returns `Container!`; engine appends `withWorkdir("/scratch")` | `core/sdk/embedded_runtime.go:166`, `core/sdk/consts.go:6` | entrypoint argv must use absolute paths |
| Only `Runtime` is implemented — no codegen, no typedefs | `core/sdk/embedded_runtime.go:78-100` | typedefs are discovered by running the container |

**`currentModule` is the trap.** It resolves, but to the *user's* module being
loaded, not to this SDK: anything the runtime wants from its own repo has to be
a literal in the file. Generation asserts the emitted file contains no
`currentModule` (§7.2).

## 3. Why this supersedes the runtime-module proposal

`runtime-module.md` proposed `github.com/dagger/typescript-sdk/runtime` as an
ordinary Dang module ref. Embedding keeps every argument it made and adds:

- **No module resolution at all.** No git fetch, no pin, no `dagger.lock` entry,
  no second ref that can drift from the SDK's — its §9.1 "two modules that must
  stay in step" problem stops existing, because the runtime is written by the
  same pass that writes `sdk/`.
- **No unpinned-ref exposure.** An embedded file is committed and reviewed.
- **Specialization becomes possible.** A shared runtime module serves every
  module, so it must detect. A generated one serves exactly one module, so it
  can be told (§7.1). This is where most of the latency goes (§8).

## 4. Scope: install and run, nothing else

**The runtime never generates anything.** `dagger-module.toml` modules build
from committed files, and the engine never passes `introspectionJson` to an
embedded runtime — so there is no codegen path to port, no
`skipRuntimeCodegen`/`RuntimeTrustsCommittedFiles` logic to satisfy, and no
introspection schema to reason about. `introspectionJson` exists in the
signature because the engine's validator requires it; the body ignores it.

Everything under `sdk/typescript/runtime` that exists to generate — `Codegen`,
`GenerateClient`, `lib_generator.go`, `introspector.go`, `SetupContainer`'s
four-goroutine codegen path, the config *writers* — is out of scope entirely.
The only upstream code with a counterpart here is
`setupContainerWithoutCodegen` (`runtime_node.go:149`, and the bun/deno twins),
which is ~40 lines each.

**Legacy `dagger.json` modules are not ours.** They keep `sdk = "typescript"`
and the engine's builtin runtime end to end — that is already how `mod.dang:114`
routes them. We emit nothing for them and carry no compatibility branch: seeing
a `dagger.json` means "the engine handles this module", full stop.

So the whole runtime is: read two things off the module source, assert the
committed files are there, assemble a container, set an entrypoint.

## 5. Three runtimes, one per JavaScript runtime

Node, Bun and Deno stop sharing a file. Upstream shares one because each runtime
also carried a codegen path worth factoring; with codegen gone, what remains is
three different container recipes that have almost nothing in common — different
base image, different install tooling, different loader, different config file.
A single file with a three-way branch would only be a way to ship two thirds of
a runtime that will never execute.

Each source lives at `runtime/<js-runtime>/main.dang` (§7.1). Everything in
`#<config>` is replaced by generation with literals for the module being
generated; the values below are the defaults that keep the file valid on its own.

### 5.1 Node — `runtime/node/main.dang`

```dang
type TypescriptNodeRuntime {
  #<config>
  let baseImage: String! = "node:24.13.1-alpine@sha256:4f696f…"
  let tsxVersion: String! = "4.22.4"
  let packageManager: String! = "yarn"      # yarn | npm | pnpm
  let packageManagerVersion: String! = "1.22.22"
  let installs: Boolean! = false            # module declares dependencies
  let moduleName: String! = "module"
  #</config>
  …
}
```

| | |
| --- | --- |
| base image | digest-pinned default, or `node:<version>-alpine` when `dagger.runtime = "node@<version>"`, or `dagger.baseImage` verbatim — **decided at generation** |
| shared prefix | `apk add ca-certificates` → `NODE_OPTIONS=--use-openssl-ca` → `npm install -g tsx@<pinned>` (§6.1). Ordered before anything module-specific so the layer is shared by every Node module on the engine |
| install (only when `installs`) | yarn: `yarn install --prod` · npm: `npm install --omit=dev` (preceded by the version-pin exec) · pnpm: `npm install -g pnpm@<v>` then `pnpm install --shamefully-hoist=true --prod` |
| install inputs | `[<lockfile>, .npmrc]` mounted from the module source |
| cache mount | yarn `/root/.cache/yarn` · npm `/root/.npm` · pnpm `/root/.pnpm-store`, shared, upstream names kept so the cache is shared with the builtin runtime |
| entrypoint | `tsx --no-deprecation --tsconfig <mod>/tsconfig.json <mod>/__dagger.entrypoint.ts` |
| committed files asserted | `sdk/client.gen.ts`, `__dagger.entrypoint.ts`, `tsconfig.json` |

The three package managers stay in one file behind the baked `packageManager`
constant: they differ by two execs and a cache path, and splitting them into
five sources would multiply the surface for no gain. A module that installs
nothing (§6.2) never reaches that branch at all.

### 5.2 Bun — `runtime/bun/main.dang`

| | |
| --- | --- |
| base image | `oven/bun:1.3.0-alpine@sha256:37e6b1…`, or `oven/bun:<version>-alpine`, or `dagger.baseImage` |
| shared prefix | none — bun runs TypeScript with decorators natively, so there is no loader to install and no CA exec upstream |
| install (only when `installs`) | `bun install --no-verify --omit=dev --omit=peer --omit=optional`, inputs `[bun.lock, bunfig.toml]` |
| cache mount | `/root/.bun/install/cache`, shared |
| entrypoint | `bun run <mod>/__dagger.entrypoint.ts` |
| committed files asserted | `sdk/client.gen.ts`, `__dagger.entrypoint.ts`, `tsconfig.json` |

### 5.3 Deno — `runtime/deno/main.dang`

| | |
| --- | --- |
| base image | `denoland/deno:alpine-2.5.0@sha256:8f58f3…`, or `dagger.baseImage` **from `deno.json`** |
| shared prefix | none |
| install (only when `installs`) | `deno install --node-modules-dir=auto` |
| cache mount | `/root/.deno/cache` — upstream names this volume `mod-bun-cache-<bunVersion>` (`runtime_deno.go:25`); fix it on the way over |
| entrypoint | `deno run -q -A <mod>/__dagger.entrypoint.ts` |
| committed files asserted | `sdk/client.gen.ts`, `__dagger.entrypoint.ts`, `deno.json` (**no** `tsconfig.json`) |

Deno resolves `@dagger.io/dagger` through `deno.json` `imports`
(`"./sdk/index.ts"` — a path, not an npm specifier), so a module with no npm
dependencies needs no install at all: `installs` is false and `deno install`
never runs.

### 5.4 The shared body

Identical in all three, ~30 lines:

```dang
pub moduleRuntime(modSource: ModuleSource!, introspectionJson: File): Container! {
  let src = modSource.{{ contextDirectory, sourceSubpath, sdk.{{ debug }} }}
  …assert committed files…
  let ctr = base                              # image + shared prefix (cached across modules)
    .withWorkdir(modulePath)
    .withMountedDirectory(modulePath, source)  # mounted, not copied
    .withMountedDirectory(modulePath + "/node_modules/@dagger.io/dagger", source.directory("sdk"))
    .withEntrypoint(entrypointArgv)
  if (src.sdk.debug) { ctr.terminal } else { ctr }
}
```

## 6. The two engine-image assets we lose

This is what makes the TypeScript port harder than Python's. Today's runtime is
handed the engine's SDK rootfs as `sdkSourceDir` and mounts two things out of
it. An embedded runtime has no such directory — and cannot read its own module
source either (§2).

### 6.1 `tsx` (Node only)

`runtime_node.go:35` mounts `/tsx_module` from the engine image and symlinks
`/usr/local/bin/tsx`. Options, in order of preference:

1. **Install it in the shared prefix**: `npm install -g tsx@<pinned>`, before
   anything module-specific. Then it is content-addressed on (image digest, tsx
   version) alone, so it runs once per engine and every Node module reuses the
   layer.

   **Measured** on the dev engine (`v1.0.0-beta.12`, linux/arm64):
   `npm install -g tsx@4.22.4` on `node:24.13.1-alpine` is **3.6s cold** (3
   packages); a second container with a different workdir reports that exec
   `CACHED`. One-time per engine, not per call. Layer ordering is what makes
   that true — a `withWorkdir` or mount placed before the install forks the
   layer per module and turns 3.6s into a per-module cost.
2. **Publish our own base image** from this repo — node + tsx + ca-certificates,
   digest-pinned — so the cost folds into the image pull and the `apk add`
   exec disappears too. Restores exactly the property the engine image gave us,
   at the cost of an artifact to build, version and keep in step with
   `tsdistconsts`. Take it if the numbers (§9.5) say the 3.6s matters.
3. **Vendor tsx into `sdk/`** at generation time. Removes the fetch, but adds
   ~MBs per module to an artifact already ~3.9 MB, and tsx ships a native
   (esbuild) binary per platform, so the committed copy would have to cover
   every platform a module can run on.
4. **Node's native type stripping** — rejected: `--experimental-strip-types`
   refuses legacy decorators, and the decorators are not optional (they are what
   registers the user's classes at import time).

**Recommend (1)** now, with the version baked by generation; keep (2) in
reserve. Bun and Deno need no loader, so this is a Node-only cost.

### 6.2 `typescript` — a hard prerequisite

All three runtimes mount the engine's prebuilt `/typescript-library` when the
module pins the default TypeScript version, and **skip installation entirely**
when that is the only dependency (`runtime_node.go:332,391,451`, and the bun and
deno twins). We write exactly that pin today: `helpers/config-updater/main.go:154`
adds `dependencies.typescript = "5.9.3"` to every generated module.

Ported naively, every `dagger call` becomes a real npm install — a regression,
not the speedup this is for.

The dependency is one import edge wide, and it is dead code at runtime:

- `library/bundle/core.js` carries 9 top-level `import … from "typescript"`
  statements (the packager builds with `--external=typescript`,
  `.dagger/modules/packager/main.dang:67`), all in the introspector region at
  the tail of the file.
- In the sources, `typescript` is imported by 15 files, **every one of them
  under `library/src/module/introspector/`**.
- The single edge that pulls that region into the bundle is
  `library/src/index.ts` exporting `entrypoint`, whose module imports `scan`
  from the introspector (`library/src/module/entrypoint/entrypoint.ts:8`).

ESM resolves static imports at load, not at use, so the specifier has to resolve
even though nothing calls it. That, and nothing else, is why every generated
module declares a TypeScript dependency.

> **This is not the generated entrypoint.** `entrypoint` here is the *library
> function* `@dagger.io/dagger` exports — the legacy dynamic dispatcher that
> scanned the user's module at call time. The generated
> `__dagger.entrypoint.ts` is a different thing entirely and stays exactly as it
> is: it is what the runtime executes, and its import list
> (`templates/entrypoint_functions.go`, `plannedImports`) is `Context`,
> `Error as DaggerError`, `FunctionCachePolicy`, `TypeDefKind`, `connection`,
> `dag`, `getRegisteredClass` — `entrypoint` is not among them. Removing the
> export removes runtime introspection, which the static entrypoint already
> replaced; it does not remove the entrypoint file.

So: build a runtime-only bundle (`library/src/runtime.ts`, the barrel minus
`export { entrypoint }`), drop the matching declaration from `core.d.ts` and the
re-export from `library/bundle/index.ts`, and stop pinning `typescript` in
config-updater. Then a default module declares **no dependencies at all**,
generation bakes `installs = false`, and the runtime skips package-manager setup
and install outright — in all three runtimes, including the deno
`imports.typescript = "npm:typescript@5.9.3"` we write today.

If it slips, the fallback is measurable rather than fatal: `npm install
--omit=dev` of `typescript@5.9.3` on the pinned node image is **1.6s cold**
(measured, same engine), cached per module manifest. But it is per module, where
the tsx layer is per engine — so it is the one worth fixing properly.

## 7. Generation

### 7.1 Detect once, at generation

Everything `analyzeModuleConfig` (`config.go:102`) does per call — six
sequential engine round-trips, then JSON parsing of `package.json`/`deno.json`,
then three detection passes — happens once, at generation, in Dang:

| decision | rule (unchanged from `config.go`) |
| --- | --- |
| JS runtime | `dagger.runtime` in `package.json` (`node@x` / `bun@x`) → `bun.lock`/`bun.lockb` → `package-lock.json`/`yarn.lock`/`pnpm-lock.yaml` → `deno.json`/`deno.lock` → **node** |
| package manager | bun/deno runtime → their own; `packageManager` field (`name@version`, version required) → `package-lock.json` → `yarn.lock` → `pnpm-lock.yaml` → **yarn 1.22.22** |
| base image | `dagger.baseImage` (`deno.json` for deno, else `package.json`) → `<runtime>:<version>-alpine` when a version was pinned → digest-pinned default |
| `installs` | does the module declare any dependency after §6.2 (`dependencies` in `package.json`, npm specifiers in `deno.json` `imports`) |

Implementation notes:

- Do it **in Dang, with `JSON.decode(contents) :: T`** — no helper container, no
  exec. Dang v2 has a map type (`Map[String!]`), so `dependencies` decodes
  directly and `installs` is `.length > 0`. (`runtime-module.md` §7.2 says Dang
  has no map type; that is out of date as of `dang/v2 v2.1.3`.)
- `existingModuleConfig` (`typescript-sdk.dang:591`) already reads exactly the
  files this needs.
- `ModConfig` (`mod-config.dang`) stays as it is: it serves the user-facing
  `mod config` surface and needs the Go helper for *writes*. Its `runtime`
  detection (`mod-config.dang:135`) is a subset of the table above — worth
  reconciling so the two cannot disagree, but they read different sources (a
  workspace vs. a generated tree), so keep them as separate functions.
- `deno.json` may legally contain comments (JSONC), which `JSON.decode` rejects,
  exactly as upstream's `encoding/json` does today (`config.go:175-186`). Same
  behavior, better error.

### 7.2 Splice and emit

Mirrors python-sdk#24: read the chosen source, replace the `#<config>` block
with literals, prepend the generated-file header, assert self-containment.

```dang
let embeddedRuntime(cfg: RuntimeConfig!): String! {
  let source = currentModule.source.file("runtime/" + cfg.jsRuntime + "/main.dang").contents
  let opening = source.split("#<config>")
  let closing = (opening[1] ?? "").split("#</config>")
  if (opening.length != 2 or closing.length != 2) {
    raise "runtime/" + cfg.jsRuntime + "/main.dang must fence its config in exactly one #<config> block"
  }
  let assembled = "# Code generated by dagger. DO NOT EDIT.\n"
    + (opening[0] ?? "") + configLiterals(cfg) + (closing[1] ?? "")
  if (assembled.contains("currentModule")) {
    raise "runtime source reads its own module source; the embedded copy would not be self-contained"
  }
  assembled
}
```

The fence is what keeps each source valid as written — the committed defaults
are real values, so `runtime/node/main.dang` can be loaded and called directly
by a check (§9.1) without going near the engine's embed path.

### 7.3 Where it lands

- **At `rootPath`, not `sourcePath`.** The engine reads
  `<sourceRootSubpath>/runtime.dang` — the directory holding
  `dagger-module.toml`. Those coincide for most modules but not for a migrated
  one whose config sits in `.dagger/modules/<name>/` with `source` pointing
  elsewhere (`mod.dang:44-56`). `moduleFiles` (`typescript-sdk.dang:614`) stages
  only under `sourcePath` today, so this is a second staging op —
  `withNewFile("/" + rootPath + "/runtime.dang", …)`.
- **Emitted for every `dagger-module.toml` module**, whatever its current
  `[runtime] source` says. The file is inert until the config points at it,
  which makes migration "regenerate, then flip one line".
- `targetRuntime` (`typescript-sdk.dang:24`) becomes `"embed:runtime.dang"` —
  one constant, since all three variants share the filename.
- `.gitattributes`: `/runtime.dang linguist-generated`. `.gitignore`: it must
  **not** be ignored (§10.4).

## 8. Optimizations

The point of the exercise. In rough order of expected value:

**8.1 — No SDK load at all.** Today's builtin runtime is a Go module, so loading
it means the Go SDK compiles it in a container, with its own module cache and
dependency closure, before the engine can ask it anything
(`runtime-module.md` §8.1). An embedded file is evaluated in-process. Largest
cold-start win, and it hits every first TypeScript module call on a fresh engine.

**8.2 — Zero detection at call time.** §7.1. No `package.json` read, no JSON
decode, no lockfile probing, no base-image resolution: all of it is a literal in
the file. What remains is one projection —
`modSource.{{ contextDirectory, sourceSubpath, sdk.{{ debug }} }}` — where
upstream does six sequential round-trips.

**8.3 — Zero-dependency modules skip installation.** §6.2, decided at generation
so the install step is not merely skipped but *absent* from the emitted file.
For a default module in any of the three runtimes this removes package-manager
setup and install entirely.

**8.4 — A shared, module-independent base prefix.** For Node, the image +
`apk add ca-certificates` + `npm i -g tsx` prefix must come before any
module-specific operation, so all Node modules on an engine share one cached
layer (§6.1). Bun and Deno have no prefix at all — `container.from(<digest>)`
and straight into the module.

**8.5 — Mount, don't copy.** Upstream overlays the module tree with
`withDirectory(".", source)`, which copies into the layer. `withMountedDirectory`
avoids the copy; the engine's own Python runtime already does it this way.

**8.6 — Stable layer ordering.** Mount `sdk/` (changes only on regeneration)
separately from the module source (changes on every edit), so editing user code
invalidates neither the bundle mount nor the install layer.

**8.7 — Digest-pinned images.** Carry `tsdistconsts`' pins over. A user pinning
`dagger.runtime = "node@22"` gets `node:22-alpine` resolved per call — upstream
behavior, kept for parity, but it is a cache miss the default does not have.

**8.8 — Cheap existence checks.** The committed-file assertions are the only
reads left in the runtime. Prefer one `entries`/`glob` over three separate
`exists` calls, and keep the actionable message
(`config.go:530`) — a missing generated file is the one failure mode users hit.

## 9. Testing

1. **Direct.** Give each `runtime/<js-runtime>/` a `dagger-module.toml` so its
   `moduleRuntime` can be called from a check against a fixture `ModuleSource`,
   asserting entrypoint argv, mounts, env and install execs — per runtime and,
   for node, per package manager. No embed-capable engine needed. (This also
   keeps `runtime-module.md`'s module-ref path alive as a debugging escape
   hatch.)
2. **Generation.** Assert the emitted `runtime.dang` sits next to
   `dagger-module.toml`, carries the generated-file marker, declares
   `pub moduleRuntime(`, contains the expected literals for that fixture (the
   *right* runtime, image and package manager), and contains no `currentModule`.
3. **Execution.** Point `.dagger/modules/runtimes/fixtures/{node,bun,deno}` at
   `embed:runtime.dang` and let the existing `nodeRunsCheck` / `bunRunsCheck` /
   `denoRunsCheck` / `invokesFunctionCheck` become the regression suite with no
   new test code. Keep one fixture on `"typescript"` so the builtin path stays
   covered.
4. **Package managers.** The current fixtures only exercise the default (yarn).
   Add npm and pnpm fixtures — a lockfile plus a `packageManager` field selects
   them — since this is the first time we own that code. Add one fixture with a
   real dependency, so the `installs = true` path is covered at all.
5. **Perf.** Cold and warm `dagger call` per runtime, embedded vs. builtin. Both
   sides of the ledger: §8.1 and §8.2 against §6.1's 3.6s.

The dev engine has embed support today —
`_EXPERIMENTAL_DAGGER_RUNNER_HOST=docker-container://dagger-engine.dev` with
`dagger-dev` (`v1.0.0-beta.12`, `dac39c69`) — so all five are reachable before
the engine change ships.

## 10. Risks and open questions

**10.1 — Specialization means regeneration is load-bearing.** A user who adds a
`bun.lock`, switches `packageManager`, or adds their first dependency without
running `dagger generate` keeps the runtime generated for the previous shape:
the wrong interpreter, the wrong installer, or no install at all. This is the
cost of §8.2 and it is deliberate — but it needs to be *loud*:
- `mod config set` (`mod-config.dang:63`) edits `package.json`; it should return
  the regenerated runtime in the same changeset rather than leave the module
  inconsistent until the next generate.
- The failure mode for "added a dependency, did not regenerate" is a module-load
  error about a missing package. Worth a check that the emitted runtime and the
  committed manifest agree, run as part of generation.

**10.2 — Engine availability.** `embed:` is unreleased. Released engines
(≤ `v1.0.0-beta.11`, what `targetEngineVersion` (`typescript-sdk.dang:36`) and
every fixture pins) reject the ref with `invalid SDK: "embed:runtime.dang"`.
Land the emit first, flip `targetRuntime` after an embed-capable release.

**10.3 — Version skew is now per module.** A module keeps the runtime it was
generated with. Hermetic and unpinnable-by-accident; also means a runtime fix
reaches a module only when someone regenerates it. The engine routes evaluation
through the Dang major ladder by the module's `engineVersion`, so language-level
compatibility is handled; semantic drift is not.

**10.4 — The `.gitignore` interaction.** The engine force-includes the file
after user patterns, but that is an *include pattern*, not a gitignore override.
The `runtimes` fixtures already ignore `/sdk` and `/__dagger.entrypoint.ts`; a
user doing the same to `runtime.dang` would get "embedded runtime file not
found" for a file that is on disk. Verify against the dev engine and make the
error actionable.

**10.5 — A generated Dang file in every module.** Specialization keeps it small
(~80–120 lines rather than ~400), which helps, but it is still a file users will
read and be tempted to edit. Header plus `linguist-generated`; nothing more.

**10.6 — Registry reachability moves.** Today a Node module can be called with
nothing but the engine image, because both `tsx` and `typescript` come out of
it. After §6.1 option (1), the first Node call on a fresh engine needs
`registry.npmjs.org`; §6.2 removes the `typescript` half, and §6.1 option (2)
converts the tsx half into an image pull. A behavior change for offline users,
and release-note material whichever option we take.

## 11. Rollout

1. **§6.2 first** — runtime-only bundle, drop the `entrypoint` export and its
   `core.d.ts` declaration, stop pinning `typescript`. Independently correct,
   and it lands while the engine's runtime still executes modules, so
   `packager:library-bundle` staleness plus the existing `runtimes` checks catch
   a mistake early.
2. **Generation-time detection** (§7.1) — in Dang, no new helper. Landable on
   its own: nothing consumes the result yet.
3. **Write the three runtimes** — node (yarn, then npm and pnpm), then bun, then
   deno, each covered by direct checks (§9.1) as it is written.
4. **Splice and emit** (§7.2, §7.3) with the generation check (§9.2). Still
   inert.
5. **Verify end-to-end against `dagger-dev`**: flip one fixture's config by hand
   and run the execution checks (§9.3), then add the missing fixtures (§9.4) and
   take the numbers (§9.5).
6. **Flip `targetRuntime`** once an embed-capable engine is released and
   `targetEngineVersion` is bumped.
7. **Leave the engine's builtin `"typescript"` runtime alone.** It serves every
   `dagger.json` module, and that is where those modules stay (§4).
