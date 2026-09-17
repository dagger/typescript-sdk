# Unified clients: scopes, roots, and one source of truth

> **Status: proposal.** Specification pass over two landed experiments —
> [dagger/typescript-sdk#54](https://github.com/dagger/typescript-sdk/pull/54)
> (one client package per module) and
> [#56](https://github.com/dagger/typescript-sdk/pull/56) (one shared client
> directory per workspace). Both work; neither is a design. #54 settled the
> *shape* of a generated client and should survive intact. #56 settled the
> *location* by hard-coding it in the TypeScript runtime, which is the part this
> replaces.
>
> This document specifies the model in terms the engine and every SDK can share,
> and then says what TypeScript does with it. Engine references are against the
> `manifest-v2` branch (dagger/dagger#14038).

## 1. Summary

Unified clients need one thing the current model cannot express: **where a
generated client lives is a separate question from who uses it.**

`clients = [...]` on a scope answers both at once today, and that is exactly why
#56 had to go around it — it hard-codes `.dagger/clients`, reads `dagger.toml`
behind the engine's back to rebuild the picture the engine already has, and has
every scope rewrite the whole shared directory because no scope owns it.

The fix is one new concept, spelled as one setting:

> A scope's **client root** is the directory its generated client packages land
> in. Scopes that share a root form a group. The engine generates each root
> **once**, from the union of its group's wants.

`clients` keeps its meaning — *what code in this scope resolves* — and gains
nothing. `clientRoot` says *where those packages are materialized*. When it is
unset the root is the scope, which is today's behavior exactly, so an SDK that
ignores this proposal keeps working (§5.4).

That one split does all the work:

| the question | answered by |
| --- | --- |
| "module A needs a client for B" | `clients` on A's scope — unchanged |
| "B's client is generated outside A" | `clientRoot` on A's scope |
| "…and is installable by anything" | the root is outside any module scope |
| "…and stays private to A" | the root is inside A's scope |
| "who owns the directory / prunes it" | the root's own generation pass, once |
| "is it idempotent" | yes: the root's contents are a function of the config |

§3 states the model, §4 the config, §5 the engine contract, §6 what TypeScript
does with it, §9 what a non-TypeScript SDK has to implement.

## 2. What the two experiments established, and what they left

### 2.1 #54 is settled and is not reopened here

Each module renders as a self-contained client package: its own root `Client`,
its own `dag`, a top-level function per root field, and a serve-on-use hook so a
client resolves wherever it is used. Cross-client object passing is structural
(`value["_ctx"] !== undefined`), so a package boundary costs nothing at runtime.

The consequence that matters here: **a module's client file is byte-identical
whichever scope asked for it.** That is what makes a shared location possible at
all — there is nothing to deduplicate, only somewhere to put one copy. §8.1
turns this from an observation into a required invariant.

### 2.2 #56 works, and cannot be the final shape

| what it does | why it cannot stay |
| --- | --- |
| every client lands in `.dagger/clients` | the path is a constant in the SDK. A user who wants a different layout — or two layouts — has no way to say so |
| the SDK reads `dagger.toml` through a Go helper (`helpers/workspace-config`) | the engine already has the scope list, resolves the targets, and pins them. Re-parsing the config in a container is a side channel that every SDK would have to reimplement |
| *every* scope renders and writes the whole shared directory | nobody owns it. Correctness rests on "the render is deterministic, so later passes are byte-quiet" — true, and still N renders of the same bytes, with a changeset that reports whichever pass ran first |
| a run from a subdirectory plans only nearby scopes (`sdkGenerationScopeApplies`, `workspace_sdk_generator.go:471`) | so the union cannot come from the planned scopes; hence the config re-read above. The cwd filter is the actual cause, and it belongs to the engine's planner |

None of these are bugs in #56. They are the symptoms of expressing a
workspace-global fact in a per-scope config.

### 2.3 What the config says today

```toml
[modules.typescript]
source = "github.com/dagger/typescript-sdk@v1"

[sdks.typescript]
module = "typescript"

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
clients = ["github.com/my-mod/B"]
```

`SDKScope` is `{IsModule, Name, Clients, Settings}` (`core/workspace/config.go:125`).
The engine resolves each `clients` entry to a `ModuleSource` and hands the scope
its own list and nothing else:

```graphql
generateScope(ws: Workspace!, isModule: Boolean!, name: String!, clients: [ModuleSource!]!): Workspace!
```

(`core/sdkmodule/provider.go:183`, shape-checked at `:281`). `ws.cwd` is the
scope; the result is diffed **root to root**, not scope to scope
(`workspace_sdk_module.go:1109`), so writing above the scope is supported and
integration-tested (`TestSDKModuleCanWriteAboveScope`,
`core/integration/generators_test.go:824`). That permission is what #56 leans on,
and §5 keeps it while removing the need to use it across scopes.

## 3. The model

### 3.1 Four words

**Scope** — a directory an SDK manages, in that language's own terms: a
TypeScript project root, a Go module, a Python distribution. Recorded in
`dagger.toml`. May hold a Dagger module (`is-module = true`), plain code, or both.

**Want** — an entry in a scope's `clients`. "Code in this scope resolves
`<module>`'s bindings." A want says nothing about files.

**Client root** — the directory where a scope's wants are materialized as
packages. Every scope has exactly one (§3.2). Several scopes may share one.

**Package** — one module's generated bindings, as a unit the language's own
tooling can resolve: an npm package, a Go module, a Python distribution. #54
already produces these; this proposal only decides where they go and who writes
them.

The two relations that `clients` conflates today:

```
scope --wants--> module          (consumption; stays in `clients`)
scope --places into--> root      (materialization; new, one per scope)
root  --holds--> package(module) (derived: the union of its group's wants)
```

### 3.2 One client root per scope

**Decided.** Not a simplification — a correctness requirement, for two
independent reasons.

*The runtime singleton.* Every generated package depends on the SDK's library
(`@dagger.io/dagger`), and the library owns the process-wide session
(`globalConnection`, `library/src/common/context.ts:6`). Two library copies in
one process means two sessions, and cross-client object passing breaks
*silently* — the `_ctx` check still passes, the IDs belong to the wrong session.
A root is self-contained (§6.2), so it carries its own library; two roots reachable
from one scope is two libraries. Forbid the second root and the hazard cannot
arise. The `globalThis` anchor proposed in `unified-clients.md` §5.1 stays worth
doing, as a net under the rule rather than a substitute for it.

*Resolution.* One root maps onto exactly one mount (`node_modules/@dagger.io`),
one import-map prefix, one `replace` block. N roots need per-package wiring and
a conflict rule for the library. There is no benefit on the other side of that
trade.

### 3.3 Private, shared, published

There is no visibility mechanism. "Private" is a **placement consequence**, and
the design is better for saying so plainly:

| root location | who can install it | travels with the module over git | copies |
| --- | --- | --- | --- |
| inside the module's own scope | the module; anything that vendors its tree | **yes** | one per module |
| a scope of its own | anything in the workspace | no (§7.1) | one per workspace |
| a registry | anything, anywhere | yes | zero generated |

Those are the same axis at three points: how far the package is reachable from.
A user picks a point per scope; §7 says which point each situation forces.

The only enforcement worth having is a validation rule, not a visibility marker:
**a scope's client root must not be inside a different module's scope** (§8.3).
Without it, module A's git context would ship module C's private clients, and
the failure would surface as an unexplained file in someone else's tree.

## 4. `dagger.toml`

### 4.1 Wants stay exactly where they are

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
clients = ["github.com/my-mod/B"]
```

Unchanged. `dagger module client add`, `rm`, `update`, `list`, `scope`
(`internal/cmd/dagger/module_sdk.go:51-114`) are unchanged. The scope-selection
rule — deepest recorded scope containing the cwd, versus the SDK's live
`findClientRoot` answer, deeper wins (`workspace_sdk_module.go:738`) — is
unchanged.

This matters more than it looks. The reason "A wants B" must stay on A's scope,
and must not migrate to the root, is that it is the only place the *intent*
lives. A root's contents are derived; wants are authored.

### 4.2 Placement is a setting

```toml
[sdks.typescript.scopes."modules/a".settings]
clientRoot = "workspace"
```

Settings already layer the way this needs: `[modules.<sdk-module>.settings]`
workspace-wide, overridden per scope by `[sdks.<alias>.scopes.<path>.settings]`
(`effectiveSDKModuleSettings`, `core/schema/workspace_sdk_init.go:82-90`), and
they arrive as the SDK module's constructor arguments. So the knob costs no new
config schema, and an SDK exposes it as an ordinary field — for TypeScript, one
more entry beside `runtime`, `template`, `entrypoint`.

Three values, because two of them are the cases that actually occur:

| value | root resolves to | meaning |
| --- | --- | --- |
| `"workspace"` *(default)* | the SDK's shared location beside `dagger.toml` — `.dagger/clients` for TypeScript | one copy per workspace |
| `"scope"` | inside the scope itself — `<scope>/.dagger/clients` for TypeScript | private; travels with the module |
| any other string | that path, resolved **config-dir-relative**, like a scope key | explicit layout |

Path values resolve against the directory holding `dagger.toml`, matching how
scope keys resolve (`ResolveSDKManagedPath`) and where `.dagger/modules` already
lives. A literal directory named `scope` or `workspace` is spelled `./scope`.

> **Aside, worth fixing either way.** `helpers/workspace-config/main.go` reads
> the SDK-wide fallback from `[sdks.<alias>.settings]`. The engine has no such
> field — `SDKEntry` is `{Module, Scopes}` (`config.go:116`) — so that table is
> reported as an unknown key by `ConfigWarnings` and never read. SDK-wide
> settings are `[modules.<sdk-module>.settings]`. Under this proposal the helper
> goes away entirely (§5.2), but the mistake is live today.

### 4.3 Worked examples

**(a) The case that has no spelling today.** A client for `B`, generated outside
module `A`, used inside it, installable by anything:

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
clients = ["github.com/my-mod/B"]
```

Nothing else. `clientRoot` defaults to `workspace`, so:

```
.dagger/clients/
  dagger/          @dagger.io/dagger        the library
  b/               @dagger.io/b             B's bindings
  a/               @dagger.io/a             A's own bindings (self client, §6.1)
modules/a/
  package.json     "@dagger.io/b": "file:../../.dagger/clients/b"
```

`npm install ./.dagger/clients/b` works from anywhere in the workspace. `A`
imports `@dagger.io/b` and resolves it through the same package.

**(b) A module that must travel over git.** One line, and the packages move
inside:

```toml
[sdks.typescript.scopes."modules/a".settings]
clientRoot = "scope"
```

```
modules/a/.dagger/clients/{dagger,b,a}/
modules/a/package.json     "@dagger.io/b": "file:./.dagger/clients/b"
```

**(c) Two consumers, one explicit root.** A module and a plain application
sharing one copy, somewhere the user chose:

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
clients = ["github.com/my-mod/B"]

[sdks.typescript.scopes."modules/a".settings]
clientRoot = "packages/dagger"

[sdks.typescript.scopes."apps/web"]
clients = ["github.com/my-mod/B", "./modules/a"]

[sdks.typescript.scopes."apps/web".settings]
clientRoot = "packages/dagger"
```

One root at `packages/dagger` holding `dagger/`, `b/`, `a/`. `apps/web` wants
`A` as well, so `A`'s package is in the union whether or not `A` is a self
client. `modules/a` and `apps/web` each get `file:` links into it.

**(d) Today's behavior, unchanged.** A workspace where no scope sets
`clientRoot` and the SDK does not implement §5.1: every scope's root is itself,
every scope materializes its own wants, nothing is shared. This is the
compatibility floor (§5.4).

### 4.4 What is deliberately *not* in `dagger.toml`

- **The union.** A root's package list is derived from the wants of its group.
  Writing it down would need reconciliation on every edit, and a hand-edit could
  make the file describe a state generation does not produce. Derived state does
  not belong in an authored file.
- **The consumer's dependency edge.** `"@dagger.io/b": "file:../../.dagger/clients/b"`
  lives in `package.json`, `deno.json`, `go.mod`, `pyproject.toml` — the file the
  language's own resolver reads. The SDK owns those entries and syncs them, the
  way `config-updater` already owns `tsconfig.json` `paths`. Duplicating them in
  `dagger.toml` would create a second source of truth for a fact the ecosystem
  already has a place for.
- **Per-package settings.** A root renders every package the same way. A package
  that needs to differ is a different root.

## 5. The engine contract

Two additions, both optional, both no-ops for an SDK that ignores them.

### 5.1 `clientRoot` — ask the SDK where a scope's packages go

```graphql
clientRoot(ws: Workspace!, isModule: Boolean!): String!
```

`ws.cwd` is the scope. Returns a workspace-root-relative path; `""` means "the
scope itself". Validated like `findClientRoot`'s result — relative, inside the
workspace — but **not** required to contain the cwd, since a shared root
generally does not.

Placement policy belongs to the SDK, not the engine: only the SDK knows that npm
resolves a `file:` dependency through its real path, or that Go wants a `replace`
to a directory with a `go.mod`. The engine reads the *result*, not the setting —
so `clientRoot = "workspace"` stays an SDK-defined word and the engine never
learns TypeScript's layout.

`isModule` is passed because a new scope may not exist on disk yet at
`dagger module client add` time, so the SDK cannot infer it from a config file.

### 5.2 `generateClientRoot` — materialize one root, once

```graphql
generateClientRoot(ws: Workspace!, clients: [ModuleSource!]!): Workspace!
```

`ws.cwd` is the root. `clients` is the **complete desired set**: the union of
every want of every scope in the root's group, plus the self client of every
module scope in the group (§6.1). Resolved and pinned by the engine through the
same path as `generateScope`'s argument (`resolveSDKModuleScopeClients`,
`workspace_sdk_generator.go:436`).

Called for every known root on every run, *including when the union is empty* —
that is how a root that has lost its last want gets removed rather than
lingering. Result validated like `generateScope`'s: cwd unchanged,
`dagger.toml` untouched, diffed at the workspace root.

This replaces `helpers/workspace-config` outright: the union the SDK was
re-deriving from TOML is now the argument.

### 5.3 Planning: the root is an inclusion edge, not an ordering edge

Two distinct graph questions, and conflating them is where this would go wrong.

**Ordering.** A root that holds a package for a *local* module depends on that
module's scope having generated first — its bindings come from that module's
schema. This is the edge `sdkModuleGraphDependencies`
(`workspace_sdk_generator.go:231`) already builds; it just moves from the
consuming scope to the root. Roots sort after the modules they hold. A consuming
scope does **not** depend on its root's contents — it only needs the root's
*path*, to write a `file:` specifier and a mount — so there is no cycle when a
module both places into a root and is held by it.

**Inclusion.** `sdkGenerationScopeApplies` (`:471`) plans only scopes that
contain, or are contained by, the invocation cwd. Running `dagger generate`
inside `modules/a` must still regenerate the root at `.dagger/clients`, or A's
packages go stale. So: **a planned scope pulls in its root**, through the same
`requireScope` transitive closure that already exists (`:344`).

The root's `clients` argument is computed from the **whole config**, never from
the plan. That is the precise correction of #2.2's last row: the union is
workspace-global because the config is, and the cwd filter decides what runs,
not what a root contains. Nothing is pruned because you happened to `cd`
somewhere.

### 5.4 Compatibility, and one naming cleanup

An SDK that implements neither function gets root == scope for every scope, the
engine calls only `generateScope`, and the behavior is byte-for-byte today's.
The Go and Python SDK modules are still on the older `initModule` / `initClient`
/ `generate` interface and are unaffected until they adopt it.

**Naming.** `findClientRoot` (`provider.go:117`) does not find a client root. It
finds the *scope* containing the cwd — the engine even names the variable
`scope` (`workspace_sdk_module.go:764`). Keeping both names invites exactly the
confusion this document is about. Proposed: rename it `findScope`, and free
"client root" for the thing that is one. It is a breaking change to the
SDK-module interface, so it should ride with `clientRoot` landing or not at all;
the alternative is naming the new function something worse.

## 6. TypeScript

### 6.1 What a client root contains

```
<root>/
  dagger/               @dagger.io/dagger    library: runtime bundle + core bindings
  <module>/             @dagger.io/<module>  one per module in the union
  node_modules/@dagger.io/*                  generated links, package -> sibling
```

Exactly #56's layout, which is the right one — real packages with `exports`,
`main` and `types`, `file:` dependencies on the library and siblings, so a
consumer runs `npm install <root>/<module>` and nothing else. The generated
`node_modules` inside the root is not a workaround to remove: npm and bun
resolve a `file:` symlink's own imports from its **real path**, which lands back
in the root, so the root has to carry the links a package manager would have
created had there been a project in there to install. Measured in #56: npm
11.6.2 and bun 1.3.14 fail without them, pnpm and yarn do not.

**A module's self client is a package in its root's union, like any other.**
That is what lets `A` call itself, and what lets `apps/web` call `A`. A module
scope contributes its own module to its root's union implicitly; the engine adds
it when assembling §5.2's argument.

### 6.2 The core library: one per root

**Decided: one `dagger/` package per client root.** The alternatives and why
not:

| option | verdict |
| --- | --- |
| one per root | **chosen.** A root is installable only if self-contained; a root whose library sits in *another* root reintroduces the realpath failure #56 found (`workspace-clients.md` §5.2) |
| one per workspace, shared by all roots | rejected — breaks self-containment, for a saving that only exists when a workspace has several roots, which is the rare case |
| one per module (today's `sdk/`) | rejected — this is the duplication the feature exists to remove |
| a published `@dagger.io/dagger@<engine-version>` | **the end state.** Zero generated bytes, zero duplication, and it makes a git-consumed module work (§7.1). Out of scope: it is gated on `runtime-module.md`, and doing it in the same step means changing the layout and the thing that reads it at once |

The sizing argument is worth stating because it looks like a regression and is
not: the bundle is ~3.9 MB. Today that is 3.9 MB **per module**. Under this
proposal it is 3.9 MB **per root** — equal in the all-private configuration,
strictly better in every other, and zero once published.

**Where core comes from.** A module is served a *compatibility view* of core
keyed on the engine version it declares, which is what made #56 render a whole
workspace against a four-release-old `Container.withDirectory(directory:)` and
fail every query (`workspace-clients.md` §5.1). #56 fixed it by taking core from
the newest-versioned target. That works, and the reason it works is worth
promoting from heuristic to definition:

> **Core is a property of the engine the client talks to, not of any module it
> binds.** A root's library renders from the session's own client-facing schema.
> Each module's package contributes only its own types, rendered from the view
> that module is served under.

Stated that way, "newest target wins" stops being a tiebreak and becomes an
approximation of the right input — one that is correct whenever the newest
target is at the session's version, which is the normal case. Sourcing core
directly rather than through a target is a follow-up (§11.4).

### 6.3 Wiring a consumer

Per scope, generated from that scope's wants and its root's path:

- **`package.json`** — one `"@dagger.io/<module>": "file:<rel>"` per want, plus
  the library. The SDK owns these entries and prunes stale ones; everything else
  in the file is the user's. Same contract `config-updater` already has for
  `tsconfig.json` `paths`.
- **`deno.json`** — import-map entries against the same paths.
- **`tsconfig.json`** — `paths` only where the scope is not npm-resolvable on its
  own. With a real package in `node_modules` after an install, aliases are
  redundant; they are what makes a module's source typecheck *before* anyone runs
  a package manager, which the embedded layout got for free.

### 6.4 The entrypoint recipe

The generated `entrypoint/main.dang` builds the container a call runs in, and
every value in it is decided at generate time — that is the property
`runtime-module.md` §7.3 asks for, and it holds here. From the scope's wants and
its root:

```dang
.withMountedDirectory("/workspace", workspace.directory("/", exclude: ["**/node_modules"]))
.withMountedDirectory("node_modules/@dagger.io", workspace.directory("/.dagger/clients"))
```

One mount, because one root (§3.2). `ClientsDir` in the generator
(`helpers/codegen/generator/typescript/templates/entrypoint_dang.go:37`) is
already a single path; it stops being a constant and becomes the resolved root.

**On reading `package.json` to find the mounts.** The tempting version is to have
generation read the module's manifest and mount what it finds. Don't — not as
the source of truth. The wants are the source of truth, the manifest is
*generated from* them (§6.3), and reading back what you just wrote is a loop
that reports its own output as input. The manifest does decide one thing the
wants cannot: a `@dagger.io/*` dependency whose specifier is **not** a workspace
`file:` path came from a registry, and must be installed rather than mounted.
So the rule is: mount the root, install the rest, and let `package.json`
distinguish them — which is what it is for.

### 6.5 Generating a scope whose packages live elsewhere

One wrinkle that is not obvious and is load-bearing. Generating module `A`
requires **scanning** `A`'s TypeScript source for its typedefs, and that source
does `import { b } from "@dagger.io/b"`. The scan needs that import to resolve —
but `A`'s pass runs before the root's pass (§5.3), so the durable package may not
exist yet, or may be stale.

`A`'s generation therefore renders its own client packages into a **staged
workspace it never writes back**. This is not new machinery: `generateScope`
already does exactly this (`typescript-sdk.dang:287`, "staged on a workspace
nothing reads back … its bindings are not the ones that belong on disk"). What
changes is that the durable copy comes from the root's pass instead of the same
pass.

The render happens twice, and that is only safe because of §8.1: the render is a
pure function of the target schemas and the SDK version, so the staged copy and
the durable copy are the same bytes by construction. Schema reads are
content-addressed on the module's subtree, so the second render is mostly cache
hits.

## 7. Portability: what travels

### 7.1 A module consumed over git

The open item from `workspace-clients.md` §9, now with a name for the rule that
governs it.

When `A` is loaded from git by another workspace, its entrypoint receives the
**consumer's** `Workspace`, and the engine filters `A`'s context to its module
directory plus config — a v2 manifest admits only `name` and `entrypoint` at the
top level, so `include` is rejected outright
(`core/modules/config_format.go:226`). So:

> **A git-consumed module can only resolve clients that are inside its own
> module directory, or that come from a registry.**

Which makes `clientRoot` the knob that decides it, and gives a rule a user can
act on:

| the scope is | set `clientRoot` to | because |
| --- | --- | --- |
| a module published for others to `dagger install` | `"scope"` | the packages must travel in the module's context |
| a module used only inside its workspace | `"workspace"` *(default)* | one copy, and nothing loads it from git |
| plain application code | `"workspace"` *(default)* | it is never loaded as a module at all |

This does not *fix* git consumption of a workspace-rooted module — it makes the
failure a configuration the user chose, statable in an error message, instead of
a layout they cannot see. The real fix is publishing (§6.2), and this proposal
should not pretend otherwise.

### 7.2 Legacy `[runtime]` modules

The engine's built-in TypeScript runtime mounts `ContextDirectory().Directory(subPath)`
— only the module's own directory. A `[runtime]` module cannot reach a
workspace-level root at all, and no `include` can fix it (v2 rejects `include`;
pre-1.0 `dagger.json` modules are not workspace-managed in the first place).

**Validation, not documentation:** a scope whose module declares `[runtime]`
must have its root inside the scope. The engine can check it at the point it
reads `clientRoot`, and the message can name the setting. #56 got the same
outcome by gating the whole layout on the `entrypoint` setting; a rule about
roots is the same constraint stated where it belongs.

### 7.3 Publishing

Named as the end state in §6.2 and not designed here, but the layout must not
foreclose it. It does not: a root's packages already carry their public names and
`exports`, and the one thing that would have to change is `file:` specifiers
becoming version ranges — `ModuleGeneratorConfig.PackagedClients` is the existing
knob for exactly this class of switch.

## 8. Invariants

### 8.1 Render purity

> A package's bytes are a function of (its module's schema, the SDK version).
> Nothing about the scope that asked for it, and nothing about where it lands.

#54 achieved this — it is the observation that module and client-only scopes
produce byte-identical files. This proposal *requires* it, in two places: §6.5
renders the same package twice and needs them to agree, and §5.2 lets any root
be generated in isolation without knowing its consumers. A regression here is
not a cosmetic diff; it is a root that churns on every run.

It should be tested as an invariant rather than assumed: render one target under
two scope configurations, assert byte equality.

### 8.2 One writer per directory

Each generation pass writes only its own subtree: a scope writes its scope, a
root writes its root. The engine's root-to-root diff still *permits* more
(§2.3), and the SDK stops needing it.

This is what makes pruning trivial and correct. The root's pass receives the
complete desired set, so anything else in the root is left over and gets removed
— by one pass, with one changeset, in one place. Compare #56, where removal is a
race that whichever scope happens to generate next resolves.

### 8.3 Validation rules

Cheap to check, each one a failure mode that is otherwise silent:

1. A scope's root must not be inside a *different* module's scope (§3.3).
2. A `[runtime]` module's root must be inside its own scope (§7.2).
3. A root must not be nested inside another root — ambiguous ownership.
4. Scopes sharing a root must agree on the source for a given module name. One
   root is one namespace; two scopes binding `hello` to different refs is an
   error naming both scopes, not a last-writer-wins race. (#56 already raises
   this, in the SDK; it belongs in the engine, which has both scopes in hand.)
5. A root's own path, if it coincides with a declared scope, is that scope — its
   wants merge into the union naturally and it is generated once, not twice.

## 9. The concept, for other SDKs

Everything above in language-neutral terms. An SDK adopting unified clients
implements two functions and honors four invariants.

**Functions.** `clientRoot(ws, isModule) -> String` (§5.1) and
`generateClientRoot(ws, clients) -> Workspace` (§5.2). Both optional; omitting
both is today's per-scope behavior.

**Invariants.** Render purity (§8.1); one root per scope (§3.2); a root is
self-contained, including its copy of the library (§6.2); one runtime instance
per scope, however that language spells "one copy of a package" (§3.2).

**What a root is, per ecosystem:**

| | TypeScript | Go | Python |
| --- | --- | --- | --- |
| package unit | npm package, `exports` + `main`/`types` | a directory with a `go.mod` | a distribution with `pyproject.toml` |
| root layout | `<root>/{dagger,<mod>...}` + generated `node_modules` links | `<root>/<mod>/` per module path | `<root>/<mod>/` per distribution |
| consumer wiring | `"@dagger.io/<m>": "file:<rel>"` | `require` + `replace <mod> => <rel>` | `[tool.uv.sources] <m> = {path = "<rel>"}` |
| library package | `@dagger.io/dagger` — runtime + core bindings | `dagger.io/dagger` | `dagger-io` |
| why one root matters | one session; npm will install two copies of anything | MVS dedupes by module path, so the risk is lower — the session argument still holds | one installed distribution per environment |
| self-containment trap | `file:` symlinks resolve from the **real path** (npm, bun) | `replace` paths are relative to the *consumer's* `go.mod` | editable installs resolve from the source tree |

The trap row is the one to read carefully. Every ecosystem has a rule about
where a locally-linked package resolves *its own* dependencies from, and every
one of them makes a root that points outside itself fail in a way that only
shows up at runtime. TypeScript's version cost #56 a debugging session; the
others will have their own.

**What stays the engine's.** Scope records, want records, target resolution and
pinning, grouping scopes by root, computing the union, ordering the graph,
deciding what a given invocation plans. No SDK should reimplement any of it —
that is the specific mistake `helpers/workspace-config` represents.

## 10. Decisions

- **`clients` is not redefined.** It means "what this scope's code resolves",
  today and after. Every alternative considered — adding provenance to entries,
  moving wants onto roots, a second array — makes the authored intent harder to
  read to express something derivable.
- **Placement is a setting, not a new config field.** The layering already
  exists, the SDK already receives settings as constructor arguments, and it puts
  the knob where a user can find it next to `runtime` and `template`. The
  engine's `SDKScope` schema does not change.
- **The engine computes the union.** It has the scope list, the resolver and the
  pins. An SDK parsing `dagger.toml` in a container is a side channel every SDK
  would have to reimplement and keep in sync with a format it does not own.
- **One root per scope.** §3.2. The runtime-singleton hazard is silent, and
  silent hazards get designed out, not documented.
- **The library is a per-root package, and a published package later.** §6.2.
- **"Private" is placement, not visibility.** §3.3. Backed by one validation
  rule instead of a mechanism that would have to be enforced at resolution time
  in three ecosystems.
- **`findClientRoot` should be renamed `findScope`.** §5.4. Breaking, small,
  and the alternative is two functions whose names claim to do the same thing.

## 11. Risks and open questions

**11.1 — Render purity is assumed, not enforced.** §8.1 and §6.5 both rest on
it. Today it holds by construction (#54) and is observed on a manual repro, but
nothing tests it. Add the invariant test before the double-render in §6.5 lands,
or a subtle per-scope difference becomes a root that rewrites itself every run.

**11.2 — Two renders per module.** §6.5 renders a scope's packages once staged
and once durably. Content-addressed schema reads should collapse most of it, but
it is a real cost on a cold cache and should be measured on a workspace with
several module scopes before this is called free.

**11.3 — The `clientRoot` question is asked per scope, per run.** It loads an
SDK module and calls a function for every scope, including scopes the run will
not generate (their roots may still be needed, §5.3). Caching is the engine's
call; worth confirming it is not a per-scope container build for SDKs less
in-engine than Dang.

**11.4 — Core from the session, not from a target.** §6.2 defines core as the
session's schema; the implementation still derives it from the newest-versioned
target. They agree whenever a target is at the session's version. Sourcing it
directly needs a schema read that is not tied to a `ModuleSource`; until then the
definition is right and the mechanism is an approximation, which should be said
in a comment where it happens rather than discovered again.

**11.5 — Git consumption stays open.** §7.1 turns it into a documented
configuration rather than a silent failure. It is not a fix, and it should not
be counted as one when this ships.

**11.6 — Migration churn.** Every workspace regenerating after this rewrites
`sdk/` and `clients/` into a root. Expected, large in user repos, and belongs in
release notes — the same warning `unified-clients.md` §9.5 already carries, now
with a second layout change behind it.

**11.7 — Two SDKs, one root.** Nothing here forbids a TypeScript scope and a Go
scope resolving to overlapping directories. The engine groups per SDK, so they
would each generate and each prune, alternately deleting the other's files.
Validation rule 3 covers nesting within one SDK; across SDKs it needs a check the
engine is well placed to do and this document does not specify.

## 12. Rollout

Each step is landable and leaves a working system.

1. **Rename `findClientRoot` → `findScope`** (§5.4), alone, while it is still a
   one-line change in one SDK module. Doing it later means doing it alongside a
   function whose name it would otherwise collide with.
2. **Add `clientRoot` to the SDK-module interface** (§5.1) plus the engine-side
   grouping and the §8.3 validation rules — with every SDK still returning `""`.
   Behavior is unchanged; the plumbing is in place and testable on its own.
3. **Add `generateClientRoot`** (§5.2) and the planner's inclusion edge (§5.3).
   Still unused: with root == scope everywhere, the engine calls it for a root
   that is the scope, which is the existing path.
4. **TypeScript returns a real root** and moves materialization into
   `generateClientRoot`. This is where #56's behavior is reproduced through the
   contract instead of around it, and where `helpers/workspace-config` is
   deleted. The existing e2e client checks are the regression suite; they need
   their `clientDir` constant to become a function of the scope's setting again.
5. **The invariant test for §8.1**, before or with step 4.
6. **Expose `clientRoot` as a user-facing setting** with the §4.2 values, and
   document the §7.1 table. Until this step the default is the only reachable
   value, which is fine — it is the one #56 already ships.
7. **Publishing** (§7.3), gated on `runtime-module.md`, which is what removes
   the library from every root and closes §7.1 properly.

## 13. What was not verified

This is a design document, not a test report. Almost everything above is read
from source rather than executed, and the distinction matters for anyone
deciding how much of it to trust.

- **Nothing in §5 was run.** `clientRoot` and `generateClientRoot` do not exist.
  The claim that they compose with the existing planner rests on reading
  `workspace_sdk_generator.go` and `core/sdkmodule/provider.go` on the
  `manifest-v2` branch, not on a prototype.
- **The ordering argument in §5.3 is reasoning.** That a consuming scope needs
  only its root's *path* — and therefore that scope → root is not an ordering
  edge — follows from §6.5's staged render being sufficient for the typedef
  scan. That is how the current code behaves, but the combination has never run.
- **The npm/bun/pnpm/yarn table and the stale-core failure are #56's findings**,
  reproduced here from its PR description. They were verified there, against a
  live engine; they were not re-run for this document.
- **The Go and Python rows in §9 are inference** from those ecosystems'
  documented resolution rules, not from an implementation. They are the part of
  this document most likely to be wrong.
- **The `[sdks.<alias>.settings]` finding in §4.2** is read from
  `helpers/workspace-config/main.go` against the engine's `SDKEntry` struct and
  `ConfigWarnings`. The conclusion that it warns rather than errors follows from
  `ConfigWarnings` being a warning path; no `dagger.toml` was fed through it to
  watch it happen.
- **The ~3.9 MB bundle figure in §6.2** is carried over from `module-gen.md`,
  not measured here.
