# Generate a module entrypoint (manifest v2)

> **Status: proposal.** Rewritten onto engine PR
> [dagger/dagger#14038](https://github.com/dagger/dagger/pull/14038)
> (`manifest-v2`, open), which replaces the module *runtime* contract with a
> module *entrypoint*. Supersedes both [`runtime-module.md`](./runtime-module.md)
> (runtime as a git-ref Dang module) and the embedded-runtime shape this file
> previously described
> ([#14018](https://github.com/dagger/dagger/pull/14018), `embed:<filename>`):
> an entrypoint source directory is a strict generalization of a single embedded
> file, and it returns types and dispatches calls instead of returning a
> container.
>
> Spec: `future/module-manifest-v2/spec.md` on that branch. Example:
> `future/module-manifest-v2/example-go-sdk.md`.
>
> **Reads against [#13992](https://github.com/dagger/dagger/pull/13992)**
> (`sdk-ux-module-max`), which replaces the SDK-module interface this repo
> implements, and against its TypeScript counterpart
> [typescript-sdk#42](https://github.com/dagger/typescript-sdk/pull/42), which is
> the SDK-side baseline everything here builds on. The two engine PRs **disagree
> about the manifest** — see §7.1, the one thing to settle before building.

## 1. Summary

A manifest v2 module names an **entrypoint** instead of a runtime:

```toml
manifestVersion = 2
name = "hello"

[entrypoint]
kind = "dang"
source = "./entrypoint"
```

The entrypoint is a directory of Dang files that we generate. It implements two
functions — `types()` returns the module's type definitions, `call()` dispatches
one constructor or function. The engine loads it directly and **never calls the
SDK module** to load or run a module.

Two consequences dominate everything below:

1. **Type discovery stops needing a container.** Today, loading a TypeScript
   module means building a container, installing dependencies, starting node,
   and running the generated `register()` so it can chain `dag.typeDef()` calls
   back at the engine. Under manifest v2 the typedefs are *generated Dang
   literals*, evaluated in-process. `dagger functions`, `dagger call --help`,
   loading a module as a dependency, and every workspace-wide traversal stop
   touching a container at all.
2. **The container becomes a private implementation detail of `call()`.** There
   is no runtime contract, no implicit JSON file protocol, no `FunctionCall`
   round-trip. Our entrypoint builds whatever container it wants, execs a
   dispatcher with the call on stdin, and returns the JSON it prints.

Everything the previous drafts said about *where the runtime lives* is obsolete.
What survives unchanged: the two engine-image assets we lose (§8), and the
principle that all detection happens at generation time (§6.1).

## 2. The contract

| Rule | Where | Consequence for us |
| --- | --- | --- |
| Manifest v2 accepts **only** `manifestVersion`, `name`, `entrypoint.{kind,source}` | `core/modules/config_format.go:230` | no `engineVersion`, no `source`, no `include`, no `[[dependencies]]`, no `[runtime]` (§10.1, §10.2) |
| `kind` is `dang` (implemented) or `module` (not yet) | `core/sdk/entrypoint.go:22` | we generate Dang |
| `source` is a manifest-relative local path that must stay inside the module dir, or an address resolving to a `Directory` | `core/sdk/dang/v2/entrypoint.go:232` | `./entrypoint` |
| The dang driver loads **every `.dang` file in the directory as one program** | `:336` | multi-file allowed; not a module; cannot declare dependencies |
| Exactly one type implements `ModuleEntrypoint`, constructible empty | `:374` | one generated type, all fields defaulted |
| `types(workspace: Workspace!): [TypeDef!]!` | `:324` | evaluated in-process, **no container** |
| `call(workspace, receiverType, receiverValue, fnName, fnArgs): JSON!` | `:324` | we build the container here |
| Zero or one object constructor; one is exposed under the module name | `:524` | unchanged from today's single-object rule |
| Name-only `TypeDef` references bind by kind + original name | spec §Type rules | `typeDef.withObject("Hello")` as a reference is enough |
| Entrypoint sees the module-facing deps schema | `:129` | core API + the module's dependencies |
| Entrypoint gets `currentWorkspace` as `workspace` | `:193` | workspace boundary can sit **above** the module dir |
| Self-calls are always enabled for entrypoint modules | `core/sdk/entrypoint.go:76` | pairs with [`unified-clients.md`](./unified-clients.md) §5.6 |
| `dagger.json` selects the legacy loader | spec §Compatibility | legacy modules stay with the engine's builtin runtime, end to end |

Call encodings are the ones we already produce and consume: `receiverValue` is
today's `FunctionCall.parent` encoding, `fnArgs` is a JSON **object** keyed by
original argument name, the result is today's `returnValue` encoding, and a void
result is `null`.

## 3. What changes for TypeScript

| Today | Manifest v2 |
| --- | --- |
| `[runtime] source = "typescript"` → engine's builtin TS runtime (a Go module the engine compiles) | `[entrypoint] kind = "dang", source = "./entrypoint"` → generated Dang, evaluated in-process |
| `register()` inside `__dagger.entrypoint.ts` chains `dag.typeDef()…` **in a container** to declare types | `types()` in Dang returns the same typedefs as literals, **no container** |
| Container polls `dag.currentFunctionCall()`, reads `parentName`/`name`/`inputArgs`, writes `returnValue`/`returnError` | dispatcher reads one JSON request on **stdin**, writes one JSON result on **stdout** |
| Runtime container assembled by the engine's TS runtime from detected config | container assembled by our generated `call()`, with the recipe baked at codegen |
| Module dir is what reaches the container (`ContextDirectory().Directory(subPath)`) | the **workspace** is passed in — files above the module dir are reachable (§10.3) |
| Types require install + node start on every load | types are free; the container only exists for real calls |

Codegen-side, that is roughly: `entrypoint_typedef.go` retargets from TypeScript
to Dang, `entrypoint_functions.go` loses its `register`/`connection`/`FunctionCall`
half, and a new template renders `call()`.

## 4. The generated tree

```
hello/
├── dagger-module.toml          # manifestVersion 2 + [entrypoint]      (generated)
├── entrypoint/
│   └── main.dang               # types() + call()                       (generated)
├── __dagger.dispatch.ts        # stdin → invoke → stdout                (generated)
├── sdk/                        # bundle + bindings, unchanged           (generated)
├── package.json / tsconfig.json                                         (generated)
└── src/index.ts                # user code                              (author)
```

Two naming notes:

- `entrypoint/` sits next to the manifest, matching the engine's own example
  (`.dagger/modules/tiny/entrypoint/main.dang`). Keeping it out of `sdk/` keeps
  the Dang program out of the tree we mount as `node_modules/@dagger.io/dagger`.
- `__dagger.entrypoint.ts` should be renamed `__dagger.dispatch.ts`. "Entrypoint"
  now means the Dang program the engine loads; the TypeScript file is the
  dispatcher that program shells out to — the same split, and the same name, as
  the Go example's `cmd/hello-dispatch`. Regeneration must prune the old file.

## 5. `types()` — the simplification

This is where the change pays. The typedefs are already known at generation
time: the introspector scans the user's source into `typedef.json`, which
`entrypoint_typedef.go` renders today as TypeScript `dag.typeDef()` chains that
run *in the container at load time*. The same data now renders as Dang, and runs
in the engine:

```dang
type Entrypoint implements ModuleEntrypoint {
  pub types(workspace: Workspace!): [TypeDef!]! {
    [
      typeDef
        .withObject("Hello", sourceMap: sourceMap("src/index.ts", 4, 14))
        .withConstructor(
          function("", typeDef.withObject("Hello"))
            .withArg("baseImageAddress", typeDef.withKind(TypeDefKind.STRING_KIND),
                     defaultValue: ("\"alpine:3.21\"" :: JSON!)),
        )
        .withFunction(
          function("container", typeDef.withObject("Container"))
            .withDescription("A container with the source mounted, ready to build on."),
        ),
    ]
  }
  …
}
```

Everything the current renderer emits has a one-to-one Dang form: objects,
fields, functions, arguments, defaults, descriptions, deprecations, source maps,
cache policies, enums, and interfaces. It is a retarget of one template, not new
semantics.

What it buys:

- **No container for type discovery.** `dagger functions`, `dagger call --help`,
  `dagger call <fn> --help`, loading the module as a dependency of another
  module, and workspace-wide operations that load every module all become pure
  in-engine evaluation.
- **No install, no node, no session round-trips** for those paths — today
  `register()` makes one API call per typedef node.
- **A missing or gitignored `sdk/`** no longer breaks type discovery; only
  `call()` needs the committed tree. That defuses half of the `.gitignore` trap
  (`runtime-module.md` §9.3).

**Alternative considered:** commit `sdk/typedefs.json` and have a hand-written,
module-independent Dang builder decode it. Smaller generated diffs and one
implementation to maintain, but building an arbitrary TypeDef tree from decoded
JSON requires recursion over a dynamic shape in Dang, against a builder API
whose calls differ per kind. Rendering literals is a straight port of code we
already have. Start with literals; revisit if the emitted file gets unwieldy.

## 6. `call()` — the container, baked per module

```dang
pub call(
  workspace: Workspace!,
  receiverType: String!,
  receiverValue: JSON,
  fnName: String!,
  fnArgs: JSON!,
): JSON! {
  let request = JSON.encode({{
    receiverType: receiverType,
    receiverValue: receiverValue,
    fnName: fnName,
    fnArgs: fnArgs,
  }})
  (runtime(workspace)
    .withExec(
      ["tsx", "--no-deprecation", "--tsconfig", "tsconfig.json", "__dagger.dispatch.ts", "engine-call"],
      stdin: request,
      experimentalPrivilegedNesting: true,
    )
    .stdout :: JSON!)
}
```

`runtime(workspace)` is a private helper in the same file holding the container
recipe — image, loader, dependency install, mounts — with every value decided at
generation (§6.1). The exec is the only per-call operation, so the whole build
above it is shared across calls and across modules that share a prefix.

### 6.1 Detection stays at generation time

Unchanged decision from the previous draft, and it matters more here because the
recipe is now *ours*: the emitted entrypoint carries answers, not rules. It
never re-derives the JS runtime, the package manager, the base image, or whether
the module installs anything. Changing any of those inputs means re-running
`dagger generate`, the same rule that already governs `sdk/` and the dispatcher.

Detection happens once, in Dang, at generation, with `JSON.decode` over the
committed `package.json` / `deno.json` — rules unchanged from upstream
`config.go`:

| decision | rule |
| --- | --- |
| JS runtime | `dagger.runtime` in `package.json` (`node@x` / `bun@x`) → `bun.lock`/`bun.lockb` → `package-lock.json`/`yarn.lock`/`pnpm-lock.yaml` → `deno.json`/`deno.lock` → **node** |
| package manager | bun/deno → their own; `packageManager` field (`name@version`) → lockfile sniff → **yarn 1.22.22** |
| base image | `dagger.baseImage` (`deno.json` for deno, else `package.json`) → `<runtime>:<version>-alpine` → digest-pinned default |
| installs | does the module declare any dependency (after §8.2 a default module declares none) |

### 6.2 One recipe per JS runtime

The three JavaScript runtimes share nothing but the mount layout, so the
generator has three `runtime()` templates and emits one:

| | Node | Bun | Deno |
| --- | --- | --- | --- |
| image | `node:24.13.1-alpine@sha256:4f696f…` | `oven/bun:1.3.0-alpine@sha256:37e6b1…` | `denoland/deno:alpine-2.5.0@sha256:8f58f3…` |
| prefix | `apk add ca-certificates`, `NODE_OPTIONS=--use-openssl-ca`, `npm i -g tsx@<pin>` (§8.1) | none | none |
| install (only when needed) | yarn `yarn install --prod` · npm `npm install --omit=dev` · pnpm `npm i -g pnpm@<v>` + `pnpm install --shamefully-hoist=true --prod` | `bun install --no-verify --omit=dev --omit=peer --omit=optional` | `deno install --node-modules-dir=auto` |
| cache | `/root/.cache/yarn` · `/root/.npm` · `/root/.pnpm-store` | `/root/.bun/install/cache` | `/root/.deno/cache` (upstream misnames this volume `mod-bun-cache-…`, `runtime_deno.go:25`) |
| dispatcher exec | `tsx --no-deprecation --tsconfig <tsconfig> __dagger.dispatch.ts engine-call` | `bun run __dagger.dispatch.ts engine-call` | `deno run -q -A __dagger.dispatch.ts engine-call` |

Mounts, identical across the three: the module tree (excluding `node_modules`),
plus `sdk/` at `node_modules/@dagger.io/dagger`. Mounted, not copied.

### 6.3 The dispatch protocol

Request on stdin, one JSON object:

```json
{ "receiverType": "Hello", "receiverValue": "{…}", "fnName": "message", "fnArgs": "{…}" }
```

Result on stdout, one JSON value. Logs on stderr. Nonzero exit is a function
error. That is the whole protocol — no session channel, no result field.

The dispatcher keeps: `connection()` (user code calls `dag`), the per-object
`rebuild`/`serialize` helpers, and `__loadCoreObject` for ID-carrying arguments
and fields. It loses: `register()`, `dag.currentFunctionCall()`, `parentName` /
`name` / `inputArgs` reads, `returnValue`, and `returnError`.

It should also keep a **developer mode**, as the Go example does:

```console
$ npx tsx __dagger.dispatch.ts call message --giant
```

Same generated dispatch code, invoked by hand — the first time a TypeScript
module author can run a function without the engine loading the module.

## 7. Generation and the manifest

### 7.1 The two engine PRs disagree about the manifest

Settle this first: it decides whether §10.1 and §10.2 are problems at all.

| | #14038 (`manifest-v2`) | #13992 + `sdk-helpers` (`cli-1.0.md:632`) |
| --- | --- | --- |
| version marker | `manifestVersion = 2`, required | none — "the TOML schema has no explicit manifest version" |
| legacy fields | **rejected**: only `manifestVersion`, `name`, `entrypoint.{kind,source}` (`core/modules/config_format.go:230`) | **coexist**: `[entrypoint]` sits beside `[runtime]`, `[[dependencies]]`, `engineVersion`, `source` |
| old engines | reject the manifest | ignore `[entrypoint]`, use `[runtime]` |
| new engines | entrypoint path | prefer `[entrypoint]`; error, never fall back, if it is invalid |
| dependencies | nowhere to declare them | `[[dependencies]]`, unchanged |

The `sdk-helpers` builder implements the second model today: `ModuleManifest`
has `withDangEntrypoint(source)` / `withModuleEntrypoint(source)` alongside
`withLegacyRuntime(…)`, validates that *at least one* is present, and emits no
`manifestVersion`. On #14038's branch such a file parses as a v1 manifest and
its `[entrypoint]` table is **silently ignored** — `validateCurrentModuleConfigTOML`
does not reject unknown keys and `CurrentModuleConfig` has no `entrypoint` tag.

The additive model is much better for us: a single generated module keeps
working on engines that predate entrypoints, dependencies and `engineVersion`
survive, and `source` stays available for migrated layouts. This design assumes
it wins; if the versioned model wins instead, §10.1 and §10.2 become blocking.

### 7.2 Plumbing (post-#13992)

The SDK-module interface this repo implements is being replaced —
`findClientRoot(ws)` plus `generateScope(ws, isModule, name, clients) -> Workspace!`,
with `defaultModulePath` optional — and typescript-sdk#42 has already migrated
us to it (`detectScope`, `generateScope`, `moduleFiles(ws, scope, sourcePath,
name, rt)` returning a `Workspace`, and `seedModule` writing the manifest
through the `sdk-helpers` builder). So the entrypoint work lands on top of that,
not on the older `generateAll`/`moduleFiles` shape:

- **The manifest is already ours to write.** `seedModule` calls the builder with
  `.withRuntime(source: runtimeSource)`; adopting an entrypoint is
  `.withDangEntrypoint("./entrypoint")` in the same call — plus, under the
  additive model, keeping the legacy runtime beside it for older engines.
- **`runtimeSource` / `targetRuntime` stays** as long as we emit a `[runtime]`
  table for older engines; it becomes dead the day we stop.
- **`generateScope` gains one output**: `entrypoint/main.dang`, written into the
  scope next to the manifest. The engine sets `Workspace.cwd` to the scope before
  generation and the SDK may write anywhere in the workspace, so this is one more
  `withFile` in the path that already writes the manifest.
- **Where the implementation lives is ours.** The manifest's `source` (kept under
  the additive model) and the generated recipe must agree — the recipe bakes it
  (§6.1), so `moduleSourcePath` is read once at generation.
- `.gitattributes`: `entrypoint/** linguist-generated`, and neither the
  entrypoint nor the manifest may be gitignored.

## 8. The two engine-image assets we lose

Unchanged from the previous draft in substance — the `call()` container still
needs a TypeScript loader and must not need the TypeScript compiler — but the
blast radius is now much smaller: neither cost is paid when a module is merely
loaded, only when a function actually runs.

### 8.1 `tsx` (Node only)

`runtime_node.go:35` mounts `/tsx_module` out of the engine image; a generated
entrypoint has no engine assets. Install it in the shared prefix instead —
`npm install -g tsx@<pinned>`, before anything module-specific, so the layer is
content-addressed on (image digest, tsx version) alone.

**Measured** on the dev engine (linux/arm64): `npm install -g tsx@4.22.4` on
`node:24.13.1-alpine` is **3.6s cold**; a second container with a different
workdir reports the exec `CACHED`. One-time per engine, shared by every Node
module — provided nothing module-specific precedes it in the chain.

In reserve: publish a digest-pinned base image (node + tsx + ca-certificates)
from this repo, folding the cost into the image pull. Rejected for now:
vendoring tsx into `sdk/` (native per-platform binary), and Node's
`--experimental-strip-types` (refuses the legacy decorators that register the
user's classes). Bun and Deno need no loader.

### 8.2 `typescript` — half done already

> **Status: the bundle half landed in typescript-sdk#42.**
> `library/src/index.ts` no longer re-exports `entrypoint`, the committed
> `library/bundle/core.js` is ~2400 lines smaller and contains **zero**
> `from "typescript"` imports, and `packager:module-bundle-check` keeps it that
> way. What remains is the consequence: `helpers/config-updater/main.go:154`
> still pins `dependencies.typescript` in every generated `package.json`, so
> modules still declare a dependency they no longer use — and under an entrypoint
> that is a real install on every call. Drop the pin.

All three runtimes mount the engine's prebuilt `/typescript-library` when the
module pins the default TypeScript version, and skip installation when that is
the only dependency (`runtime_node.go:332,391,451`). We write that pin today
(`helpers/config-updater/main.go:154`).

The dependency was one import edge wide and dead at runtime:

- `library/bundle/core.js` carried 9 top-level `import … from "typescript"`,
  all in the introspector region (the packager builds with
  `--external=typescript`, `.dagger/modules/packager/main.dang:67`).
- In sources, `typescript` is imported by 15 files, every one under
  `library/src/module/introspector/`.
- The single edge pulling that region in was `library/src/index.ts` exporting
  `entrypoint`, whose module imports `scan`
  (`library/src/module/entrypoint/entrypoint.ts:8`) — the export #42 removed.

> **Not the generated entrypoint.** `entrypoint` here is the *library function*
> `@dagger.io/dagger` exports — the legacy dynamic dispatcher that scanned the
> module at call time. The generated dispatcher never imports it
> (`plannedImports` is `Context`, `Error as DaggerError`, `FunctionCachePolicy`,
> `TypeDefKind`, `connection`, `dag`, `getRegisteredClass`), and under manifest
> v2 it needs even less than that.

With that edge gone, dropping the config-updater pin is the whole remaining
task: a default module then declares no dependencies, generation bakes "no
install", and `call()` skips package-manager setup and install outright in all
three runtimes. It is also worth doing **before** any entrypoint work — it makes
today's engine runtime stop installing too.

Fallback if it slips: `npm install --omit=dev` of `typescript@5.9.3` is **1.6s
cold** (measured), cached per module manifest — per module, where the tsx layer
is per engine.

**Note:** with types no longer needing the container, `FunctionCachePolicy` and
`TypeDefKind` leave the dispatcher's import list as well — the remaining runtime
surface is `connection`, `dag`, `getRegisteredClass`, `Context`, and the error
type. Worth re-checking what else the runtime-only bundle can drop.

## 9. Testing

1. **Types, offline.** Assert the generated `entrypoint/main.dang` reproduces the
   module's typedefs — compare `dagger functions` output against the typedef JSON
   the introspector produced. No container involved, so this is fast enough to
   run per fixture.
2. **Generation.** Assert the emitted manifest is exactly the four v2 values, the
   entrypoint declares `implements ModuleEntrypoint` with both functions, and the
   baked recipe matches the fixture's runtime, image and package manager.
3. **Execution.** Point `.dagger/modules/runtimes/fixtures/{node,bun,deno}` at
   manifest v2 and let `nodeRunsCheck` / `bunRunsCheck` / `denoRunsCheck` /
   `invokesFunctionCheck` (`.dagger/modules/runtimes/main.dang`) become the
   regression suite. Keep one fixture on the legacy path while it exists.
4. **Package managers and dependencies.** Today's fixtures only cover yarn with
   no dependencies. Add npm, pnpm, and one module with a real dependency, so the
   install path is covered at all.
5. **Dispatch protocol.** Unit-level: feed the dispatcher a request on stdin and
   assert the JSON result, the constructor case (`fnName: ""`), a null-valued
   argument, an omitted argument, and a failure exiting nonzero.
6. **Perf.** Cold and warm `dagger functions` and `dagger call` per runtime,
   legacy vs. v2. The type-discovery numbers are the headline.

> The dev engine currently available (`dagger-dev`, `v1.0.0-beta.12`,
> `dac39c69`) is built from the **embed** branch, not `manifest-v2`. Exercising
> any of this end to end needs an engine built from #14038.

## 10. Risks and open questions

**10.0 — Which manifest model ships.** §7.1. Everything below marked
*versioned-only* disappears under the additive model, so this is the first
question to ask, not the last.

**10.1 — Where do dependencies come from?** *(versioned-only.)* A
`manifestVersion = 2` manifest rejects `[[dependencies]]`, and the workspace
config has no per-module dependency list either (`core/workspace/config.go:81`).
Our module codegen generates `sdk/<dep>.gen.ts` from the module's dependency
closure, and #42's own `dagger.toml` documents modules that still depend on each
other through the manifest — so under the versioned model, either dependencies
become workspace-scoped or they move somewhere else entirely. It also interacts
with [`unified-clients.md`](./unified-clients.md), where each module client
already carries its own `MODULE_REF`/`MODULE_PIN` and can serve itself. Under
the additive model, nothing changes.

**10.2 — `engineVersion`.** *(versioned-only.)* `targetEngineVersion`
(`typescript-sdk.dang:36`) pins the release the committed bundle is built for,
and `seedModule` writes it into every manifest. A versioned v2 manifest has no
field for it, making the pairing between a generated module and the engine that
runs it implicit.

**10.3 — Which workspace, and what about remote modules?** `call()` receives
`currentWorkspace` — the *caller's* workspace. That is a real gain for local
modules (a monorepo's root `package.json` / `pnpm-workspace.yaml` is finally
reachable), but a module consumed as a git dependency needs its own source, which
that workspace does not contain. The mechanism that should cover it is
`currentModule.source` inside the entrypoint — the nested client is served with
the module context (`core/sdk/dang/shared/shared.go:53`) — but this is exactly
the thing the embedded-runtime design forbade, so **verify it first**: it decides
whether the recipe mounts the workspace, the module source, or both.

**10.4 — Two moving contracts at once.** The SDK-module interface is being
replaced by #13992 (`findClientRoot` / `generateScope`) at the same time as the
module-loading contract is replaced by #14038 (`types` / `call`). #42 lands the
first; this design lands the second on top. They are independent — but the
entrypoint work should not start until #42 settles, or it rebases twice.

**10.5 — Error fidelity.** Today the dispatcher returns structured errors
(`dag.error(msg).withValue(k, v)` via `returnError`, carrying extensions). Under
v2 a failure is "nonzero exit + stderr", surfaced as the exec error. Confirm
whether structured error values survive, or whether we lose extensions.

**10.6 — Regeneration is load-bearing.** Adding a `bun.lock`, switching
`packageManager`, or adding a first dependency without re-running
`dagger generate` leaves a stale recipe. Deliberate (§6.1); it needs to be
visible where those inputs change, e.g. `mod config set`
(`mod-config.dang:63`).

**10.7 — Two "entrypoints" in one tree.** §4 renames the TypeScript file to
`__dagger.dispatch.ts`; if we keep the old name, every conversation about this
module has to disambiguate. Renaming means pruning the old file on regeneration.

**10.8 — Registry reachability.** After §8.1, the first Node *call* on a fresh
engine needs npm; today both assets came from the engine image. Type discovery
needs neither, which softens it considerably.

## 11. Rollout

0. **Land typescript-sdk#42** — the module-max interface is the floor this
   builds on, and the runtime-only bundle came with it (§8.2).
1. **Drop the `typescript` pin** from `helpers/config-updater`. What is left of
   §8.2, independently correct, and it makes today's engine runtime stop
   installing too.
2. **Settle §7.1** — versioned vs. additive manifest — and with it §10.1 and
   §10.2. Then **§10.3**: whether `currentModule.source` is reachable from an
   entrypoint, which decides what the recipe mounts.
3. **Retarget the typedef renderer to Dang** (§5), against the same `typedef.json`
   the dispatcher renderer already consumes. Testable offline with golden files
   before any engine supports it.
4. **Simplify the dispatcher** (§6.3) and add developer mode.
5. **Render `call()` per JS runtime** (§6.2) with generation-time detection
   (§6.1), and write the entrypoint into `generateScope` (§7.2).
6. **Verify end to end** against an engine built from #14038, then flip the
   fixtures (§9.3) and take the numbers (§9.6).
7. **Legacy `dagger.json` modules stay with the engine's builtin runtime** and are
   not our problem.
