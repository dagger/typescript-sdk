# Splitting `@dagger.io/dagger`

**Goal.** A generated client should `import` its core bindings from a *published*
package instead of carrying a generated copy. That means `@dagger.io/dagger` has
to stop being one package that contains six things.

**Status.** Ownership, the generator question, bootstrapping, deno's
dependency-age floor and the embedded runtime's lifetime are all settled. The
dual-package hazard is measured rather than assumed, on npm, bun and deno — see
"The hazard, measured". Those results come from a throwaway harness that models the
proposed package graph against a local verdaccio; the method is described where the
results are, so it can be rebuilt.

**Steps 1 and 2 have landed** — `library/src` is laid out in layers, the dead
subgraph is gone, and `e2e:registry` runs a real registry so the package graph can
be tested by installing it rather than by reasoning about it. Several claims below
moved from argued to measured as a result, and one of them changed. Nothing
published has changed. Sequencing is at the end.

## What is actually tangled

Measured from `library/src` before the split, not guessed. The paths below are the
pre-split ones; the arrows are what made the layering obvious:

```
telemetry/        → otel only, zero dagger imports              standalone today
common/errors/    → graphql, graphql-request
common/graphql/   → errors, connectOpts, otel/api, node-fetch   (provisioning via dynamic import)
common/context.ts → common/graphql/*                            Context, BaseClient, ServeSpec
api/client.gen.ts → common/context.ts — and nothing else        4.3 MB of the bundle
connect.ts        → context, graphql/*, telemetry, client.gen (Client type only)
provisioning/     → errors, graphql/client, connectOpts + adm-zip, tar, execa, env-paths
module/registry+decorators → reflect-metadata + 1 error class   tiny
module/entrypoint/         → registry, client.gen, connect, telemetry, introspector
module/introspector/       → the TypeScript compiler            generation-time only
```

Two things fall out of this, and they set the whole shape of the design:

1. **The bindings are already a leaf.** `client.gen.ts` imports exactly one
   module: `Context`/`BaseClient`. There is nothing to untangle to get the
   bindings out — the seam is already cut. Same direction the Go SDK just took:
   `dagger.io/dagger` keeps the connection, `dagger.io/dagger/core` holds the
   generated types and imports it ([`013ea54145`][go-split] in dagger/dagger).
2. **The only genuinely tangled piece is the introspector, and it is
   generation-time.** It is already kept out of the shipped bundle
   (`library/VENDOR.md`, `src/index.ts` delta), and a static entrypoint never
   calls it. It does not need to be in any published package.

So this is a distribution problem, not a refactor problem. The hard parts are
ownership, local development, and singletons — not the import graph.

[go-split]: https://github.com/dagger/dagger/commit/013ea54145

## Proposed packages

| Package | Contents | External deps |
|---|---|---|
| `@dagger.io/session` | `Context`, `BaseClient`, `Connection`, `computeQuery`, errors, `ConnectOpts`, `connect`/`connection`, **provisioning** | `graphql-request`, `graphql`, `node-fetch`, `@opentelemetry/api`, `adm-zip`, `tar`, `execa`, `env-paths`, `node-color-log` |
| `@dagger.io/core` | generated core bindings + `dag` | `@dagger.io/session` |
| `@dagger.io/module` | decorators, registry | `@dagger.io/core`, `reflect-metadata` |
| `@dagger.io/telemetry` | otel wiring | the otel SDK |
| `@dagger.io/dagger` | **facade.** re-exports all four | the four above |

Naming: `session` rather than `client`, because "client" already means "generated
bindings" everywhere else in this repo (`clients/`, client-gen, unified clients).
`core` matches the Go SDK, so the two SDKs tell the same story.

**Provisioning stays in `session`** (decided). It is already behind a dynamic
`import()`, so it does not load unless a program needs to spawn its own engine.
Splitting it would buy back `adm-zip`/`tar`/`execa`/`env-paths` — maybe 10% of
what this split sheds, next to the TypeScript compiler and the otel SDK — at the
cost of a declared cycle, one more package to publish in order, and a worse
failure when a standalone script is missing it.

### Who installs what

| Consumer | Today | After |
|---|---|---|
| Generated client package | whole bundle + its own generated `client.gen.ts` | `@dagger.io/session` + `@dagger.io/core` |
| Module (user source) | whole bundle, per scope | `@dagger.io/dagger` facade — **imports unchanged** |
| Standalone script | whole bundle | `@dagger.io/dagger` facade — unchanged |

**Users never type `@dagger.io/core` or `@dagger.io/module`** (decided).
`@dagger.io/dagger` stays the user-facing name permanently. The default template
never changes.

A generated client's closure drops the introspector, the TypeScript compiler and
the otel SDK. It also drops six dependencies declared today and imported nowhere:
`@grpc/grpc-js`, `@lifeomic/axios-fetch`, `graphql-tag`,
`@opentelemetry/exporter-jaeger`, `@opentelemetry/sdk-metrics`,
`@opentelemetry/semantic-conventions`. The reverse drift exists too: `telemetry/`
imports `@opentelemetry/exporter-trace-otlp-proto` and `sdk-trace-base`, neither
declared, and `session/utils.ts` imports `node-color-log`, which the table above
now lists. Splitting forces honest manifests, which is how you find both kinds.

**What that is worth, measured.** Each closure installed on its own with
`npm install --omit=dev` (npm 11.6.2, macOS arm64), sizes from `du -sk`:

| Closure | Installed | Packages |
|---|---|---|
| today's manifest — what every generated client gets | **161 MB** | 160 |
| `session` (and `core`, which adds nothing of its own) | **29 MB** | 45 |
| `telemetry` | 96 MB | 74 |
| `module` (`reflect-metadata` alone) | 0.3 MB | 1 |
| `typescript`, for reference | 24 MB | 1 |

So a generated client goes from 161 MB to 29 MB — **5.5× smaller, and 115 fewer
packages**. The estimate this replaces said "roughly half the weight is otel"; it
is closer to 60%, and the otel closure alone is over three times the size of
everything a client actually needs.

**npm refused today's manifest — fixed.** The measurement needed
`--legacy-peer-deps` to run at all: `graphql-request@7.4.0` declares
`peer graphql@"14 - 16"` and the library declared `graphql@^17.0.1`. bun and yarn
install that tree without a word, which is why it went unnoticed; npm is the only
one of the three that enforces peers on install, and every consumer of a published
`session` would have hit `ERESOLVE` on the first one.

There is no `graphql-request@8` to move to, and the library's own use of `graphql`
is two `import type`s, so the range only ever had to satisfy the runtime
dependency: it is now `^16.14.2`. `bundle/core.js` got 182 KB smaller as a side
effect. `packager:manifest-installs-check` resolves the production closure with
real npm so the next such bump fails here rather than at a user.

### These packages cannot ship TypeScript source

The package a scope carries today has `main: ./index.ts`, and that works only
because nothing resolves it *as* a package: the engine mounts `sdk/` into
`node_modules` and runs the entrypoint through tsx, and a generated entrypoint
installs its own tsx (`dangTsxVersion`, `entrypoint_dang.go`). Publish the same
shape, install it, and **two of the three runtimes refuse it** at the entry:

| Runtime | Loading the package as published |
|---|---|
| node | **refused** — `ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING` |
| node + tsx | ok |
| bun | ok |
| deno | **refused** — the same error code, through its own resolver |

```
Error [ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING]: Stripping types is currently
unsupported for files under node_modules, for
"file:///work/node_modules/@dagger.io/dagger/index.ts"
```

Not read from anybody's docs — published to the step 2 harness and imported under
each of them. All four outcomes are asserted in
`e2e:registry:publish-install-check`, the refusals by the path they name rather
than by their wording. deno matching node here was the surprise: it makes this a
property of the ecosystem rather than a node quirk to work around.

A module never meets it, whichever runtime it targets — node goes through tsx, and
deno resolves `@dagger.io/dagger` through `deno.json` to a path, where the rule does
not apply. A **client-only scope** does: there the user runs their own node or deno
and `@dagger.io/core` sits in their manifest like any other dependency. So `core`,
`session` and the facade have to ship compiled JavaScript with declarations beside
it — the shape `library/package.json` already describes (`main: dist/src/index.js`,
`files: ["dist/"]`) and that nothing in this repo currently builds. Adding that
build is part of step 3 rather than a detail of it, and it also lands on the
dagger/dagger pipeline that generates `core`: generating the bindings is not
enough, it has to compile them.

## Who publishes what

The split line is not "who wrote the code". It is **what the version number
means**.

| Package | Published from | Version means |
|---|---|---|
| `@dagger.io/core` | **dagger/dagger** | the engine release — it *is* the core schema |
| `@dagger.io/dagger` | **dagger/dagger** | the engine release — it pins core exactly |
| `@dagger.io/session` | **typescript-sdk** | its own semver, schema-independent |
| `@dagger.io/module` | **typescript-sdk** | its own semver |
| `@dagger.io/telemetry` | **typescript-sdk** | its own semver |

`core` has to be cut by whatever cuts engine releases, because a release is the
event that changes it. The facade pins `core` exactly, so its version is also the
engine version — publishing it elsewhere opens a lag window where
`@dagger.io/dagger@latest` points at a stale core.

One `@dagger.io/core` per engine release, starting at the first v1 (decided).

### Layout in dagger/dagger

The frozen pre-v1 tree and the new split cannot both be `sdk/typescript` — they are
different packages with different contents. The frozen one moves aside (decided):

```
sdk/typescript-v0/    frozen. embedded-runtime source only: no releases, no npm,
                      no tags. Dies when pre-v1 module compat does.
sdk/typescript/       the new split — keeps the name and the tag namespace
  package.json        @dagger.io/dagger (facade)
  packages/core/      @dagger.io/core
```

Two things make moving the frozen tree cheaper than leaving it:

- **It needs no release pipeline.** Pre-v1 modules never install from npm — the
  engine mounts the built directory as `node_modules/@dagger.io/dagger`
  (`engine-dev/build/sdk.go:145`, an explicit Include list over
  `source.Directory("sdk/typescript")`). Once the facade takes over the npm name,
  the frozen tree is only source to copy, so its release entry is *deleted* rather
  than re-pointed.
- **Nesting the new tree under the old one would collide with the tag namespace.**
  Release tags are path-derived — `.dagger/modules/release/main.go:294` maps
  `path: "sdk/typescript/"` to `tag: "sdk/typescript/"`, producing
  `sdk/typescript/v0.14.0`. A directory `sdk/typescript/v1` would live inside a
  string space where `sdk/typescript/v1.0.0` already means a release.

Cost is ~6 mechanical path references, all in build and release tooling and none in
the engine: `engine-dev/build/sdk.go:145`, the release module's path/tag entry,
`release/generate.go:89`, `publish_check.go:230`.

**None of this happens until this repo is set up first** — see Sequencing.

**dagger/dagger's release pipeline generates `core` by calling this repo's
module** (decided — option (b)): `dagger call -m github.com/dagger/typescript-sdk
…`. One generator, in-pipeline, no lag, no cross-repo shape contract, and it
dogfoods. This matters because the contract is class shape, not just names: a
module client generated here does `new Container(ctx)` on a class imported from
`@dagger.io/core`. Keeping both generators and golden-testing each side was the
alternative, and it drifts on the first template change nobody mirrors.

### Bootstrapping is not a problem — the schema comes from the engine

The worry was circular: `github.com/dagger/typescript-sdk` is itself a Dagger
module pinning an `engineVersion`, so releasing engine *vNext* looked like it meant
asking a module built for *vPrevious* to produce vNext's bindings.

It does not, because of where the schema comes from. Generation is two stages and
neither is pinned to the module's own engine version:

1. **The schema is introspected from the engine running the generate** —
   `moduleSource.clientSchemaIntrospectionJSON`, a live query against that engine.
   Run the generate on the new engine and you get the new schema.
2. **Codegen is engine-free.** It takes that JSON and emits files in a container
   with no engine attached — "it turns an introspection schema into generated
   bindings" (`typescript-sdk.dang:822`); the JSON is written to `/schema.json` and
   passed as `--introspection-json-path`.

So the pinned `typescript-sdk` version only has to *load* on the new engine, which
forward compatibility already gives. It reads that engine's schema and emits
bindings for it. Bump this repo's `engineVersion` afterwards if you want, but the
correctness of the bindings does not depend on it.

And because `core` is generated in dagger/dagger against the very engine being
released, the bindings and the engine cannot drift apart: they come from the same
run.

## Local development

Three different problems, three different tools. The experiments in "The hazard,
measured" rule one popular tool out.

**Inside typescript-sdk** — `session` ← `module`, `telemetry`. Use **npm
workspaces**. One install at the repo root, packages resolve to each other by
symlink, edits are live, no publish. This is the common case and it is solved.

**Across repos** — "I changed `session` and want to see it under `core`, which is
generated in dagger/dagger." Use **npm `overrides`** in the consuming project:

```json
{ "overrides": { "@dagger.io/session": "file:../typescript-sdk/library/packages/session" } }
```

`overrides` forces *every* transitive resolution to the local copy. That is
exactly right here, because it is a single-version enforcement — the same property
we want in production. **Measured: npm and bun both honour it** and collapse the
duplicate to one copy. Deno has no equivalent (see below).

**Do not use `npm link`** for this graph. It creates a second copy of `session`
alongside the resolved one, which is precisely the dual-package hazard measured
below. `overrides` replaces; `link` adds.

**End-to-end** — the honest test is "does a generated module install and run
against the real package graph", and that needs a registry. Run **verdaccio in a
container** as a Dagger function: publish the working tree's packages to it,
point a fixture module's `.npmrc` at it, run the existing e2e checks. Reproducible,
works in CI, and it doubles as the release rehearsal below. The e2e fixture
harness in `.dagger/modules/e2e` is where it slots in.

## Releasing

**Publishing never blocks on installability.** `npm publish` does not resolve
dependencies, so publishing the facade before `core` is fetchable cannot fail.
Ordering matters for *consumers*, not for the publisher.

**Each package releases independently**, and that is the point of splitting the
versions. `session`, `module` and `telemetry` cut from typescript-sdk on their own
cadence; nothing else has to move. Only `core` and the facade are coupled, and
they are coupled to the engine release, in one pipeline.

**A retry loop is the right instinct, but it only fixes one of three staleness
sources.** The experiments turned up three, and they need different answers:

| # | Source | Fixable by polling? |
|---|---|---|
| 1 | Registry propagation — not fetchable the instant `publish` returns | **Yes** |
| 2 | Client metadata caches — bun served a packument missing a just-published version until its cache was cleared | No; client-side and sticky |
| 3 | **Deno's `--minimum-dependency-age`, default 24h in 2.9.2** — it refuses any package published more recently | No; client-side policy |

**(1) Two-phase publish with a poll.** Don't gate *publishing* on installability —
`npm publish` doesn't resolve dependencies, so it cannot fail that way. Gate the
**dist-tag flip**:

1. `npm publish --tag next` for `core`, then the facade. Nobody on `latest` sees
   either.
2. Poll from a cold cache until both resolve — `npm view @dagger.io/core@X version
   --prefer-online` in a fresh container, with backoff — then run a smoke test.
3. `npm dist-tag add @dagger.io/core@X latest`, then the facade.

The user-visible switch is step 3 and takes seconds. If step 2 never succeeds you
simply never flip, and no user saw it — which matters because `npm unpublish` is
restricted to a 72-hour window and refuses outright once anything depends on the
version. `npm deprecate` is the only recourse after that.

**(2) Never mutate a published version.** Measured: republishing the same version
with changed content is served stale from client caches — npm and bun both handed
back the old tarball. Every variant gets a new version number. This is also why the
`next` → `latest` flip is the right shape: it never rewrites anything.

**(3) Deno's 24-hour floor — solved, by a config key the SDK already writes.** A
generated deno module pins `@dagger.io/core@<exact engine version>`. If that version
was published an hour ago, `deno install` refuses it outright:

```
error: Could not find npm package '@dpt/core' matching '^1.0.0'.
A newer matching version was found, but it was not used because it was newer than
the specified minimum dependency date of 2026-09-27 10:54:56 UTC.
```

Without a fix, `dagger call` on a fresh deno module breaks for ~24h after every
engine release. But `minimumDependencyAge` is a real `deno.json` key, and it takes
an `exclude` list with prefix wildcards:

```json
{ "minimumDependencyAge": { "exclude": ["npm:@dagger.io/*"] } }
```

It covers *transitive* resolution — `@dpt/session` arrived through `@dpt/core` and
was exempted with it — and it is **surgical**, which a negative control establishes
rather than assumes. Six configurations, same freshly-published packages:

| `minimumDependencyAge` | Result |
|---|---|
| absent | refused |
| `{"age": "P1D"}` | refused |
| `{"age": "P1D", "exclude": ["npm:@somethingelse/*"]}` | **refused** — the exclude must match |
| `{"age": "P1D", "exclude": ["npm:@dpt/*"]}` | ok |
| `{"exclude": ["npm:@dpt/*"]}` | ok — floor stays at deno's 24h default for everything else |
| `"0"` | ok — blanket opt-out, *not* what we want |

The third row is the one that matters: an exclude naming a different scope is still
refused, so it is the wildcard match doing the work and the floor remains in force
for every other package the user depends on. Ship the fifth form — exempt our scope,
inherit deno's default for everything else.

**Version boundary.** Deno enables the floor by default from 2.9 ("Since Deno 2.9
this is on by default with a 24-hour window"); before that there is no floor to
exempt. The key is honoured on 2.9.2, tested. A version that enforces the floor but
does not understand the key would be a gap, but the floor is *why* the key exists,
so that window should be empty.

Two paths need it and one mechanism covers both, because `deno install` reads
`deno.json` from the workdir:

| Path | Who runs the install | Covered by |
|---|---|---|
| Module under an entrypoint | the SDK — `withExec(["deno", "install", "--node-modules-dir=auto"])`, `entrypoint_dang.go:674` | the generated `deno.json` |
| Client-only scope | the user, in their own project | the generated `deno.json` |

So the change is in `updateDenoConfig` (`helpers/config-updater/main.go:474`), which
already does exactly this for the `unstable` flag list:

```go
appendIfNotExists(denoConfig, "minimumDependencyAge.exclude", "npm:@dagger.io/*")
```

No CLI flag, no new mechanism, and it composes with a user who has set their own
`minimumDependencyAge` — `exclude` is additive.

**Generated code sidesteps (1) entirely.** A generated client pins an exact
version, never `latest`, so the dist-tag flip only affects humans running
`npm i @dagger.io/dagger`.

## Compatibility

**Existing user code.** `@dagger.io/dagger` stays, as a facade that re-exports the
union. `import { dag, object, func, connect } from "@dagger.io/dagger"` keeps
working. No codemod.

The facade cannot be a bare `export *` over the four packages, and the failure is
sharper than I first wrote. `client.gen.ts` re-exports `BaseClient` and so does
`session`. Measured, on Node 24:

- `import { BaseClient } from "@dagger.io/dagger"` → **`SyntaxError: The requested
  module contains conflicting star exports for name 'BaseClient'`**, at link time.
  The whole module graph fails to load.
- `import * as dagger from "@dagger.io/dagger"` → the name is **silently absent**.
  `dagger.BaseClient` is `undefined` and it is not in `Object.keys`.

So the facade needs an explicit disambiguation block, and a test that imports every
name it claims to re-export. The silent-namespace case is the dangerous one.

**It is the only collision**, which makes that block one line rather than an audit.
Enumerated with the TypeScript checker rather than by reading: `getExportsOfModule`
over each package's entry points, then the intersection.

| Package | Exports | Contributes a colliding name |
|---|---|---|
| `session` | 19 | `BaseClient` |
| `core` | 363 | `BaseClient` |
| `module` | 17 | — |
| `telemetry` | 1 | — |

The same pass measures the other half — what a bare `export *` would *add*, since
today's `src/index.ts` is an explicit list and not a star over the layers. Eight
names are exported by a layer and withheld by the facade: `Args`, `ArgumentOptions`,
`Class`, `FunctionOptions`, `State`, `Registry` and `registry` from `module`, and
`ServeSpec` from `session`. Seven are types or a class and cost nothing either way.
The one that matters is `registry`, the mutable singleton instance — the facade
exports `getRegisteredClass` instead, and that is what keeps a user from writing
into it. `entrypoint`, the omission the code comments talk about, is no longer on
the list: step 1 deleted it.

**Legacy embedded modules** — `sdk/typescript` in dagger/dagger, mounted by the
engine as `node_modules/@dagger.io/dagger`. It does **not** consume the split, and
that is a requirement rather than a convenience: it **must** stay frozen on the
monolith it ships today for as long as pre-v1 module compat is supported. The
freeze has a known end — when that compat is dropped — and until then a bug in the
split cannot reach a module written before v1.

## The hazard, measured

`globalConnection`, the decorator `registry` and `dag` are module-scope singletons.
The claim was that two copies of `session` in one tree breaks them. Built the
graph — `core` → `session@1`, `module` → `session@2`, a facade over both, an app
over the facade — packed all five as tarballs and installed with real npm.

**npm nests the duplicate without complaint.** `npm ls --all`:

```
app@1.0.0
+-- @dpt/core@1.0.0
|   `-- @dpt/session@1.0.0
+-- @dpt/dagger@1.0.0
|   +-- @dpt/core@1.0.0 deduped
|   `-- @dpt/module@1.0.0 deduped
`-- @dpt/module@1.0.0
    `-- @dpt/session@2.0.0
```

**The hazard reproduces exactly.** The decorator writes into `session@2`'s
registry; the dispatcher reads `session@1`'s:

```
session modules evaluated: 2
  @dpt/module sees: session@2.x#1
  @dpt/core   sees: session@1.x#2
registered "Hello" via @dpt/module's decorator
dispatch("Hello") -> FAILED: registry has 0 entries (core sees session@1.x#2)
```

Note what the error says: *the registry is empty*. Nothing points at the package
graph. In the real SDK this surfaces as "my `@object` class isn't found", and you
would look at the decorator, the dispatcher, and the introspector before you looked
at `node_modules`.

**The `globalThis` fix closes it.** Keying the singletons off
`Symbol.for("@dagger.io/session.singletons")` and reusing whatever is already
there: both copies still evaluate, but they share one registry, and dispatch
succeeds.

**But the first-loaded copy wins, and that has a cost.** Whichever `session`
evaluates first defines the shared instance — including its *implementation*. If
the older copy wins, code linked against the newer one calls a method that is not
there:

```
shared registry instance came from session@1.x
object()    -> ok (present in both versions)
enumType()  -> FAILED: TypeError: registry.registerEnum is not a function
```

So `globalThis` converts a silent split-brain into a single instance that may be
the older implementation. Better, not free. It argues for keeping the shared
singletons' shape **append-only**, or storing a version alongside them and
upgrading in place.

**Peer dependencies make it loud at install time — on npm.** Declaring `session` as
a peer of `core` and `module` instead of a dependency turns the same graph into a
refusal:

```
npm error code ERESOLVE
npm error Could not resolve dependency:
npm error peer @dpt/session@"^2.0.0" from @dpt/module@1.0.0
```

### Across all three runtimes

Everything above was npm. Rerun against a local verdaccio with real semver ranges,
separating the *runtime* axis (same duplicated tree, three runtimes) from the
*package manager* axis (three installers, same manifests):

This turns up **two** distinct failure modes, not one, and they need different
answers.

**Mode 1 — duplicate copies.** Plain `dependencies`, conflicting ranges.

| | npm 11.6 | bun 1.3.14 | deno 2.9.2 |
|---|---|---|---|
| Installs two copies of `session` | yes | yes | yes |
| Hazard fires with module-scope singletons | yes | yes | yes |
| `globalThis` containment fixes it | yes | yes | yes |
| Single-version lever | `overrides` | `overrides` | **none found** |

**Mode 2 — silent version mismatch.** `peerDependencies`, conflicting ranges.

| | npm 11.6 | bun 1.3.14 | deno 2.9.2 |
|---|---|---|---|
| Refuses the install | **yes** — `ERESOLVE` | no | no |
| Installs one version satisfying only one range | — | yes | yes |
| Mismatched package fails at call time | — | `TypeError` | `TypeError` |
| `globalThis` helps | — | **no** | **no** |

Mode 2 is the one I got wrong first time round. bun and deno do not duplicate here
— they install a single `session@1` that satisfies `core`'s range, leave `module`'s
`^2` peer quietly unsatisfied, and the failure surfaces only when `module` calls
something session@1 does not have:

```
### bun, conflicting peer ranges
  session copies: 1
  @dpt/module resolved to session@1.x#1
  object()   -> ok
  enumType() -> TypeError: registry.registerEnum is not a function
```

Three things follow.

**The peer guard is npm-only.** On bun and deno `peerDependencies` does not prevent
the problem, it *changes the symptom* — from split-brain to missing API. That is
not an improvement; it is the same class of silent breakage with a different
stack trace.

**`globalThis` containment is the primary defence for mode 1**, and the only
mitigation that held on all three runtimes, because it is a language feature rather
than a resolver behaviour. It does nothing for mode 2.

**The real guarantee is that only one major of `session` is ever in the wild.**
Neither mode can occur if every `core`/`module` in circulation accepts the same
`session`. An append-only `session` API and a wide range (`^1`) is the actual fix;
`globalThis` and peer deps are damage limitation for when that slips.

**Deno has no single-version lever at all.** Its import map does not collapse a
transitive duplicate, even with an explicit `"@dpt/session": "npm:@dpt/session@1.0.1"`
entry — two instances either way. The map governs what the app's own bare
specifiers resolve to, not what its dependencies resolve to.

### What to do

In priority order, because they address different things:

1. **Keep `session` on one major, with an append-only API.** This is the actual
   guarantee — it makes both modes unreachable. Everything below is what happens
   when it slips.
2. **`globalThis`-keyed singletons.** The primary defence for mode 1, and the only
   mitigation that held on all three runtimes.
3. **`peerDependencies` on `session`.** Costs nothing and turns mode 2 into a loud
   install failure *for npm users*. Do not treat it as the guarantee — on bun and
   deno it only changes the symptom.

Add a test that asserts one shared instance under a deliberately duplicated tree,
and **run it on all three runtimes** — that is the only place the guarantee
actually lives. It belongs in the e2e suite alongside the verdaccio harness from
step 2 of the sequencing, which is where a duplicated tree can be built on demand.

**That test now exists, on all three runtimes**, in `e2e:registry`. Two intermediary
packages pin different exact versions of the library, each runtime's own installer
hoists one and nests the other, and the app imports both: the writer's copy
registers a class through `object()`, the reader's copy looks it up through
`getRegisteredClass`. The tree is built by the resolver rather than by hand, and the
copy count is asserted, because a dedupe would make it pass for nothing.

Each runtime asserts the *contrast*, not one outcome — on its own `shared true` says
very little, and a runtime that deduped two identical files would report agreement
for reasons having nothing to do with `shared`. Six trees:

| | as shipped | `shared("registry", …)` patched out |
|---|---|---|
| node | `copies 2`, `shared true` | `copies 2`, `shared false` |
| bun | `copies 2`, `shared true` | `copies 2`, `shared false` |
| deno | `copies 2`, `shared true` | `copies 2`, `shared false` |

All three duplicate, all three share with the containment and split without it. That
is the guarantee, and it is now a check rather than a claim.

**Verdict on the runtimes question: no blocker.** Nothing about bun or deno makes
the split unworkable. What changes is which mitigation carries the weight, and that
the `session` API's stability matters more than any of the tooling.

## What changes in typescript-sdk

Mostly deletion.

- `corePackage` / `corePackageJSON` / `clients/dagger/` in `typescript-sdk.dang`
  go away. Every scope stops carrying its own copy of the core.
- `clientPackageJSON`'s `"@dagger.io/dagger": "file:../dagger"` becomes real
  registry ranges: `@dagger.io/core` and `@dagger.io/session`.
- The generated client's imports split: `Context`/`BaseClient` from
  `@dagger.io/session`, `dag as __dag` from `@dagger.io/core`.
- `library/bundle/core.js` stops shipping into modules. Only the introspector
  bundle remains, and it is generation-time.
- The entrypoint install step (#57) pulls core from npm — cacheable by digest.

**Version pinning.** Generation already knows the engine version, so it pins
`@dagger.io/core` exactly. `@dagger.io/session` floats (`^1`). For a dev engine or
an unreleased schema there is no published core to pin — fall back to generating
`clients/dagger` locally exactly as today. That fallback is the current code, so it
costs nothing to keep.

One thing the experiments surfaced: **`file:` deps on a *directory* do not install
transitive dependencies.** npm links the directory and leaves its `dependencies`
unmet. Today's generated `clients/*` → `file:../dagger` is unaffected because the
local core package has no dependencies of its own. After the split it would — so
the move from `file:` to registry ranges is not optional, it is load-bearing.

## Dead code found on the way — since deleted

Reachability was computed from both real bundle entry points — `src/index.ts` →
`core.js`, and the introspector's entry → `introspector.js`. A closed subgraph
rooted at the dynamic dispatcher that `src/index.ts` stopped exporting was
unreachable from both, and step 1 removed it:

| File | Lines | Reached by |
|---|---|---|
| `module/entrypoint/entrypoint.ts` | 107 | nothing |
| `module/entrypoint/invoke.ts` | 109 | only `entrypoint.ts` |
| `module/entrypoint/context.ts` | 8 | only `invoke.ts` + `load.ts`'s dead half |
| `module/executor.ts` | 255 | only those three |
| `module/entrypoint/load.ts` | 373 → ~29 | **344 lines dead** |

`load.ts` exported eight functions and only `load()` was live —
`introspector/index.ts` imports it for `scan()`. The other seven were imported
only by `invoke.ts`. One 3-line function was keeping 344 lines *and*
`executor.ts` in the introspector bundle; it now lives beside its caller as
`introspector/load.ts`.

Two caveats:

- **`provisioning/` looks orphaned and is not.** It is reached through a dynamic
  `await import(...)` in `session/graphql/connect.ts`, which a static scan misses.
- **These files are live upstream.** `sdk/typescript/src/index.ts:22` still does
  `export { entrypoint }` — the dispatcher the embedded runtime uses for pre-v1
  modules. Deleting them here widens the vendor delta `VENDOR.md` tracks, and since
  the embedded runtime stays frozen, that delta does not resolve on a re-vendor. It
  goes away when pre-v1 compat does.

## What step 1 turned up

Two things the layering only became visible once enforced:

**`connect` was in the wrong layer.** It constructs a `Client`, so it cannot sit
below the bindings. Split: `connection` stays in `session` and only establishes a
session; `connect` moved to `core/connect.ts`. This is the same seam the Go SDK
has between `dagger.Connect` and `core.NewQuery`, arrived at independently, which
is mild evidence the layering is the natural one.

**`session` reached up into `telemetry`** — `connection` owns the tracer's
lifetime, calling `initialize`, wrapping the session in the otel context and
`close`ing on the way out. Step 1 left that edge in place and had `layeringCheck`
name it as an exception, because moving it was a behaviour change rather than a
move. It contradicted a claim made above: with it, `@dagger.io/session` depends on
`@dagger.io/telemetry`, every generated client pulls the OpenTelemetry SDK, and "a
generated client's closure drops the otel SDK" is false.

**Since fixed, and it was not a lifecycle hook.** A hook would have been a
three-method interface for one caller. `connection` is not a session feature at
all — it is a *composition* of a session and a tracer, so it belongs to neither
layer and now sits beside `index.ts` as `src/connection.ts`, which is facade code
and the only package that will carry it. What is left in `session` is
`withSession(fct, cfg)`: establish a session, point the global client at it, tear
it down. No tracing, no bindings.

Measured, by bundling each entry on its own:

| Entry | Bundled | References to the otel SDK |
|---|---|---|
| `session/connect.ts` before | 3,785 KB | 46 |
| `session/connect.ts` after (`withSession`) | **1,296 KB** | **0** |
| `connection.ts` (facade) | 3,785 KB | 46 |

So the otel SDK is ~2.4 MB of bundled JavaScript — two thirds of what a session
closure used to carry — and the claim above is now true rather than aspirational.
Nothing published changed: the facade still exports `connection` with the same
signature, and the composition is byte-for-byte the same calls in the same order.

`layeringCheck` asserts the rule instead of naming the exception: no layer may
reach `telemetry`. Files directly under `src/` are unchecked, because that is the
facade and the facade is defined as the thing allowed to depend on everything.

~820 lines that should not be carried into `@dagger.io/module`.

## The endgame worth aiming at

Once `core` and `session` are published, `library/` here stops being a vendored
copy of `sdk/typescript` and becomes the *source* of `session`, `module` and
`telemetry`, plus the introspector and the generator. The vendoring relationship
inverts: dagger/dagger consumes what this repo publishes.

Most of `VENDOR.md`'s delta-tracking burden goes with it, and so does the "two
copies of the truth" problem — for everything except the frozen embedded runtime.

## Sequencing

**typescript-sdk is set up first; dagger/dagger is not touched until it is.** The
split has to be working and published from this side before the other repo
reorganizes around it — otherwise dagger/dagger carries a half-finished dependency
and the pre-v1 tree gets moved for nothing.

*In this repo:*

1. ~~**Cut the seams in place.**~~ **Done.** `library/src` is now
   `session/ core/ module/ telemetry/`, the dead subgraph is gone, the singletons
   are `globalThis`-keyed with a duplicate-copy test, and `packager:layering-check`
   holds the direction. Not yet npm workspaces — the layering is what later steps
   need, and splitting `package.json` can wait until there is something to publish.
   One bundle still comes out; nothing published changed.
2. **Stand up the verdaccio harness** in `.dagger/modules/e2e`. Needed by local
   dev, by the duplicate-tree test, and by the release rehearsal — and it is the
   only way to test step 4 before doing it for real. *Started:* `e2e:registry`
   runs verdaccio as a service, publishes the package a scope carries today into
   it, installs it from there — which is what turned up the shape constraint above
   — holds the duplicate-tree guarantee with a negative control on each of node,
   bun and deno, and pins deno's dependency-age floor together with the key that
   clears it. What is left is the release rehearsal itself, which needs something
   published to rehearse.
3. **Publish `session`, `module`, `telemetry`.** Nothing consumes them yet, so a
   mistake here costs a version bump and nothing else.

*Then in dagger/dagger:*

4. **Move the pre-v1 tree to `sdk/typescript-v0/`** and build the facade +
   `packages/core/` at `sdk/typescript/`. Publish `core` + facade from the release
   pipeline, generated by calling this repo's module. Two-phase
   (`next` → verify → `latest`).

*Then back here:*

5. **Switch generation**: `clients/dagger` → registry ranges, with the
   local-generation fallback for dev engines.
6. **Shed the bundle.** `core.js` stops shipping; only the introspector remains.

## What I did not verify

Static claims came from reading the tree at `917aa67` and dagger/dagger at
`f36c0b37ef`. Package-graph claims were executed against npm 11.6.2, bun 1.3.14,
deno 2.9.2 and Node 24.11.1, over a local verdaccio — with model packages
(`@dpt/session`, `@dpt/core`, `@dpt/module`, a facade over both) rather than the
real SDK. Still open:

- **That `@dagger.io/core` really has no external dependency.** `core/connect.ts`
  imports `GraphQLClient` from `graphql-request` for one parameter annotation, so
  as written the package needs it. `import type` would remove it from both the
  runtime closure and the emitted `.d.ts` — one word, not yet done, and the
  manifest in the table above assumes it.
- **That the facade, once written, re-exports what it claims.** The surface is now
  enumerated — one collision, eight withheld names — but that is the *source* tree
  measured by the checker. No facade package exists yet to test against.
- **Why bun served a stale packument.** Observed against verdaccio; whether it
  reproduces against real npm, or is an etag/304 interaction with the local
  registry, is not separated. The harness now invalidates caches on publish, so it
  does not affect any result here.
- **Real-registry propagation timing.** The poll-before-flip design is built around
  a window I did not measure, because measuring it means publishing to npmjs.
- **Deno older than 2.9.2.** The key is tested on 2.9.2 and the floor only exists
  from 2.9, so the gap should be empty — but no older version was run.
- **How the engine's runtime actually mounts the SDK** — read from
  `typescript-sdk.dang`, not the engine code that implements it.

Three things that were open in earlier drafts are now settled and have moved into
the body: generation reads its schema from the engine it runs on (so bootstrapping
is a non-issue), `minimumDependencyAge.exclude` is surgical (negative control
included), and the embedded runtime staying frozen is a requirement, not an
assumption.
