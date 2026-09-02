# Unified clients: one package per module

> **Status: proposal.** Grounded in a hand-surgery spike on a generated `hello`
> module (dependency `hi`) against `v1.0.0-beta.11`: no `dagger generate` run,
> the generated tree edited by hand until `tsc --noEmit` was clean and the
> module ran end to end. The TypeScript counterpart of the Go unified-client
> experiment.

## 1. Summary

Today a TypeScript module's dependencies are **grafted onto the core client**.
`sdk/client.gen.ts` holds the core bindings; each dependency gets a
`sdk/<dep>.gen.ts` that extends the core `Client` by TypeScript declaration
merging (`declare module "./client.gen.js"`) plus JS prototype patching
(`__apply<Dep>Augmentations`), and the core file `export *`s it back out. One
`Client`, one namespace, every module merged into it.

This proposal splits that: **one client package per module** — core, each
dependency, and the module itself — each owning its own root `Client`, its own
`dag`, and its own entrypoint function, all attached to the one shared session.

```ts
import { dag, Container } from "@dagger.io/dagger"   // core
import * as hi from "@dagger.io/dagger/hi"           // dependency
import * as self from "@dagger.io/dagger/hello"      // this module

hi.hi().hello({ figletContainer: dag.container().from("alpine:3.21") })
self.hello(this.ws).baseImageAddress()
```

Three things came out of the spike that decide the shape of the work:

- **The session is already extracted**, so the split needs no runtime change
  (§3.1). Go had to pull `session.Default()` out of 19k lines of codegen; we
  ship it as a runtime library already.
- **Cross-client object passing needs no contract at all** (§3.2). Go needs an
  exported `WithGraphQLQuery` / `XXX_GraphQL*` pair; our argument serialization
  is structural.
- **A self client is just another client** (§5.6) — and it is genuinely new: the
  TypeScript SDK generates no self-call bindings today.

The cost is concentrated in codegen (§5.2–§5.5) and in one breaking change to
the module-facing import surface (§8).

## 2. What the spike established

Both demo functions passed on `dagger --x-release=v1.0.0-beta.11 api call hello …`:

| function | what it proves |
| --- | --- |
| `self-base` → `alpine:3.21` | the **self client** works: a module calling itself through a generated client |
| `message` → figlet banner | **cross-client ID passing** works: a core-built `Container` crossed into the `hi` client's `figletContainer` argument through the shared session |

A second pass externalized the runtime and core bindings as packages
(`@dagger.io/session`, `@dagger.io/core` with session as a `peerDependency`) and
re-verified both functions — that is §7, deliberately out of scope here.

## 3. Why TypeScript is already halfway there

### 3.1 The session already exists

`Context` defaults to the process-wide `globalConnection`
(`library/src/common/context.ts:6-10`), and `connection()` sets its GraphQL
client once per process from `DAGGER_SESSION_PORT`/`TOKEN`
(`library/src/connect.ts:36-52`). Two `dag` instances already coexist in every
module today — one inside the bundled runtime, one in `client.gen.ts` — and they
interoperate *only* because both bind to that singleton. Shared-by-default is
therefore already proven in production, not a new property this design
introduces.

`defaultRoot()` is `new Context()`; `selectNode(id, type)` is a method that
already exists (`context.ts:31`). The spike's `session.ts` is a naming layer, not
machinery. **The core split requires no change to `library/`.**

### 3.2 Cross-package object passing is structural

Argument serialization detects API objects by the presence of a query context
and resolves them to IDs:

```ts
const isQueryTree = (value: any) => value["_ctx"] !== undefined
```
(`library/src/common/graphql/compute_query.ts:87`)

No class identity, no nominal typing, no marshaller interface. An object built
by any client is accepted by any other client for free — one less rule than the
Go design, which has to make the construction contract explicit.

The mirror-image contract — a client *returning* a type it does not own
(`Hello.container(): Container`) — is the public `Context`-taking constructor
(`new Container(ctx)`), already public today, documented as "internal usage
only" by comment rather than by visibility.

### 3.3 The split removes an existing hazard

`BaseClient` lives in the runtime rather than in `client.gen.ts` specifically
because the core file `export *`s the dependency files, so a value import back
the other way would form an ESM cycle (`library/src/common/context.ts:46-56`).
That is also why dep files may only *type*-import the extendable classes and why
the augmentations are deferred into a function called from the core file's
footer.

Standalone packages import strictly downward — client → core → runtime — so the
cycle, and all the machinery that works around it, goes away.

### 3.4 No `New()` naming rule

Go needs `hi.New()` because `func Hi` collides with `type Hi`. TypeScript is
case-sensitive, so the package entrypoint can share the module's name:
`hi.hi().hello()`. Strictly better than the Go shape.

## 4. The target output

```
today                                      unified
─────                                      ───────
sdk/client.gen.ts    core bindings +       sdk/client.gen.ts   core bindings only:
                     dep fields on Client                      no dep re-exports, no footer
                     + export * of deps
sdk/<dep>.gen.ts     declare module +      sdk/<dep>.gen.ts    standalone client package
                     prototype patching
                                           sdk/<self>.gen.ts   NEW: the self client
sdk/index.ts         core.js + export *    sdk/index.ts        core surface only
                     client.gen.ts
tsconfig.json        2 path aliases        tsconfig.json       + @dagger.io/dagger/<mod> per module
```

Every generated client package has the same shape, whether it is a dependency,
the module itself, or (§6) the bound module of a standalone client:

```ts
export const MODULE_REF = "github.com/shykes/daggerverse/hello"
export const MODULE_PIN = "54d86c…"

export class Client {
  constructor(ctx: Context = defaultRoot()) { … }   // shared session by default
  hi(): Hi                                           // this module's schema slice
}
export const dag = new Client()                      // no I/O until the first query
export function hi(): Hi                             // entrypoint mirroring the root field
export function loadHiFromID(id: ID): Hi             // unmarshalling path
```

The self client is the same file with the constructor's arguments on the
entrypoint: `hello(ws: Workspace, opts?: HelloOpts): Hello`.

## 5. What changes in this repo

Ordered so each phase is landable on its own, except that **§5.5 must ship with
§5.2** — the split breaks the entrypoint's object loading.

### 5.1 Runtime (`library/`) — optional, small

Not required by the split, but worth taking first:

1. Export `defaultRoot()` / `selectNode()` from the barrel
   (`library/src/index.ts`) so generated code names the session explicitly
   instead of open-coding `new Context()`.
2. Export `Connection`. It is not in the barrel today, so a custom session (Go's
   `ConnectOpts{Session}`) cannot be expressed at all.
3. Anchor `globalConnection` on `globalThis`. Cheap insurance now, a
   prerequisite for §7: npm — unlike Go's MVS — will happily install two copies
   of a package, and a second copy means a second session and silently broken
   cross-client ID sharing.

Related inconsistency worth fixing while here: `connect(cb)` builds its own
non-shared `Connection` (`library/src/connect.ts:70-73`), so objects made inside
it do not share the global session.

Any change here means rebuilding `library/bundle/` through
`.dagger/modules/packager` and committing the result.

### 5.2 Codegen: the standalone client template

In `helpers/codegen/generator/typescript`:

- Replace `templates/src/_dep.ts.gtpl` **and** `templates/src/_augmentations.ts.gtpl`
  with one `_client.ts.gtpl` emitting the §4 shape. `AugmentFnName`,
  `ExtendableClassNames`, `Augmentation` and the `augmentation_*` sub-templates
  are deleted with them.
- `templates/src/header.ts.gtpl`: drop the `DependencyExports` import/re-export
  block and the whole `footer` template (the unconditional
  `__apply<Dep>Augmentations({ Client, Binding, Env })` calls).
- `generator.go:70-122` keeps its shape. The split loop already renders one file
  per module and already excludes their types from the core schema; only the
  template it renders changes. `IsExtendableType` filtering becomes irrelevant
  for module files — a module's `Query` fields become its own `Client`'s
  methods rather than augmentations on the core one.

### 5.3 Cross-module type references

Today every type is reachable from one file because the core file `export *`s
the dep files. Split, a module client that references a type owned by *another*
module must import it directly: module A depends on B and C, and B's API returns
C's type.

`CoreValueNames` / `CoreTypeNames` (`templates/functions.go:162-163`) must become
owner-aware — resolve each referenced type to its owning module and emit the
import against that file rather than always against the core file. Keep the
existing discipline: `import type` wherever the reference is only in a
signature, value imports only for what a method body constructs. Sibling
value-imports are the one place a new cycle could appear (two modules whose APIs
reference each other), so prefer type-only imports and construct through the
`Context`-taking constructor.

### 5.4 Config and dang wiring

- `helpers/config-updater` writes exactly two path aliases today
  (`main.go:20-21`). It needs a variable list: one
  `@dagger.io/dagger/<module>` → `./sdk/<module>.gen.ts` entry per module in the
  closure, for both `tsconfig` and `deno-config` modes. Resolution in the
  container flows through tsconfig `paths` (tsx runs with `--tsconfig`), so this
  is what makes the bare specifiers resolve at runtime, not just for tsc.
- `typescript-sdk.dang`: `generateModuleBindings` (`:474`) passes the module list
  through to the config step; `moduleSdkDirectory` (`:495`) is otherwise
  unchanged — it lays down whatever codegen produced. `moduleBindings` (`:665`)
  already prunes `*.gen.ts` for modules that have left the closure, so the
  changing file set needs no new machinery.

### 5.5 Entrypoint (ships with §5.2)

`__loadCoreObject` resolves a class by name off a namespace import of the whole
SDK (`templates/entrypoint_functions.go:140,468`):

```ts
import * as __dagger from "@dagger.io/dagger"
const cls = (__dagger as any)[typeName] ?? (__dagger as any)[typeName + "_"]
```

Once the core file stops re-exporting dependencies, every dependency-typed field
or argument fails at runtime with *"generated client class not found"*. The
typedef JSON carries only `{kind, name}` (`templates/entrypoint_typedef.go`), no
owning module, so the entrypoint cannot resolve this on its own.

Fix: have module codegen emit a generated loader beside the clients — a file
that imports each client package and exports
`loadObjectFromID(id, typeName)` built from the type→module map codegen already
has from the schema. The entrypoint imports that instead of doing a namespace
lookup. The dependency direction is entrypoint → clients, so no cycle, and an
explicit map avoids resolving name collisions by search order when two modules
export the same type name.

### 5.6 The self client

The blocker is the input, not the template. The module-facing schema is
**core + dependencies only**: the module's own types are not in it
(`design/module-gen.md:124-129`), which is why the spike had to hand-write
`hello.gen.ts`. Confirmed on the spike's generated tree — its `client.gen.ts`
contains neither the module's own type nor its dependency's.

`design/module-gen.md:417-423` already names the way out:
`dag.schema(deps).merge(ownTypes, name).contents`
(`core/schema/schematool.go`, gated `v1.0.0-0`) is callable **from dang**, so the
merged schema can be handed to the codegen container as data and the helper stays
engine-free. **Verify what `ownTypes` accepts at generate time before committing
to it** — the module is not loaded yet at that point. If it needs live typedefs
we do not have, the fallback is converting the introspector's `typedef.json` into
introspection types inside `helpers/codegen` and rendering the same template;
that JSON is already produced for the entrypoint, and it is the only artifact
that describes the module's own API before the module can be loaded.

Self clients are therefore the last phase, not a prerequisite for the rest.

## 6. Standalone clients get the same treatment

`codegen client` already splits **every** module — including the bound one —
into its own `<module>.gen.ts`, keeping only core types in `dagger.gen.ts`
(`generator.go:80-87`, `functions.go:772-777`). It just renders them through the
augmentation template. Switching that template over (§5.2) makes standalone
clients unified for free, and lets one improvement follow:

`serveBoundModule` and the wrapped `connection`/`connect` are emitted into the
core file today (`header.ts.gtpl`). In the unified shape that bootstrap belongs
to the bound module's own package — it is that module's concern, and every
generated module client already carries `MODULE_REF`/`MODULE_PIN` for exactly
this. Fold it in rather than porting the core-file patch.

## 7. Follow-up: session and core as external packages

Out of scope, recorded because §5.1 should not foreclose it. The spike verified
it works: `@dagger.io/session` (the runtime bundle, owning the singleton) and
`@dagger.io/core` (compiled core bindings, `peerDependencies: { "@dagger.io/session": "*" }`),
with the module keeping only its own client files, its entrypoint and config.

Two engine-runtime constraints block a local/monorepo variant today, and both
disappear once [`runtime-module.md`](./runtime-module.md) lands:

1. The TypeScript runtime slices the module subdirectory out of the context
   (`sdk/typescript/runtime/config.go:144`), so nothing above the module dir
   reaches the container — Go's `include = ["../../.."]` trick has no equivalent.
2. Only `package.json` is copied before install (`runtime_node.go:160`), so any
   `file:` dependency target is absent at install time.

Published registry packages hit neither. The real risk is npm duplication (§5.1).

## 8. Decisions

- **Keep `sdk/client.gen.ts` as the core filename.** The spike renamed it to
  `core/client.gen.ts` and needed a re-export shim to satisfy the runtime's
  file-presence check (`sdk/typescript/runtime/config.go:531`). Keeping the name
  gets the same layout with no shim and no migration artifact.
- **Flat `sdk/<module>.gen.ts`, not `sdk/<module>/<module>.gen.ts`.** Matches
  what we generate today; the path alias hides the layout either way.
- **Breaking change, accepted:** `import { Hi } from "@dagger.io/dagger"` stops
  working once the core file drops `export *`. That is the point of the split —
  the merged namespace is what makes two modules able to collide. Preserving it
  behind a generated `export *` in `sdk/index.ts` would keep the collision and
  the hazard; if a deprecation window is needed, it should be a stated release
  step with an end, not a permanent compatibility surface.

## 9. Risks and open questions

**9.1 — `ownTypes` at generate time.** §5.6. Everything about self clients
depends on it; verify before scheduling that phase.

**9.2 — Sibling import cycles.** §5.3. Two mutually-referencing module clients
are legal in the schema. Type-only imports cover the common case; a value
reference in a method body between two such modules needs a check that the
deferred-evaluation argument (bodies run after load) actually holds under both
tsx and bun.

**9.3 — Type-name collisions across modules.** Merged into one namespace today,
they are a bug; split, they are fine *except* in the entrypoint's loader
(§5.5). The explicit map is what makes that safe — do not fall back to
searching namespaces in order.

**9.4 — The entrypoint break is silent until runtime.** A module with no
dependency-typed field or argument will typecheck and run fine while the loader
is wrong. The e2e fixtures need a case that passes a dependency object *through*
the entrypoint (as a field on the module object and as an argument), not just
one that calls a dependency.

**9.5 — Regeneration churn.** Every module regenerated after this lands rewrites
its whole `sdk/`. Expected, but it is a large diff in user repos and belongs in
release notes.

## 10. Rollout

1. **§5.1 runtime exports + `globalThis` anchor.** Independently correct,
   rebuild and commit `library/bundle/`.
2. **§5.2 + §5.3 + §5.5 together** — the template swap, owner-aware imports and
   the entrypoint loader. This is the breaking change; it lands as one piece
   because the entrypoint cannot survive the template swap alone.
3. **§5.4 config-updater aliases**, with unit tests for the variable alias list.
4. **e2e coverage** for §9.4 before flipping fixtures: a fixture module that
   takes and returns a dependency object across the entrypoint boundary.
5. **§6 standalone clients** — same template, plus moving `serveBoundModule`
   into the bound module's package.
6. **§5.6 self clients**, gated on §9.1.
7. **§7 externalization**, gated on [`runtime-module.md`](./runtime-module.md).
