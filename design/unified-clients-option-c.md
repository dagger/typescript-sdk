# Option C: the package manifest is the only link

> Revised after round 2 — three drawbacks dissolved on inspection, one engine
> gap found instead. Follows [`unified-clients-take2.md`](./unified-clients-take2.md).

## 1. What it is

`dagger.toml` says **where clients are written**, and nothing else.

```toml
[sdks.typescript.scopes."."]
clients = ["github.com/my-mod/B"]      # written at .dagger/clients/

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"                              # says nothing about B
```

```
.dagger/clients/{dagger,b}/
modules/a/package.json    "@dagger.io/b": "file:../../.dagger/clients/b"   <- npm install wrote this
```

Clients you declare, the SDK wires for you; clients someone else declares, you
`npm install`.

## 2. Drawbacks, revised

| # | drawback | round 1 said | actually |
| --- | --- | --- | --- |
| 1 | the scan has no core | fatal | **dissolved — the scan never reads disk** |
| 2 | `npm install` needs a `dagger generate` | every time | **only the first install from a directory** |
| 3–4 | no graph in `dagger.toml` | medium | **accepted — it is npm's own failure mode, and loud** |
| 5 | per-SDK manifest parsing | low | accepted — it is also the flexibility |
| 6 | ordering is convention | low | **dissolved — the scope graph already replaced `generateLocalDependencies`** |
| 7 | relative paths break when a module moves | low | stands |
| 8 | *(new)* the generation fold aborts on first error | — | engine gap, not specific to C |

### 2.1 The scan never reads disk

Generation already stages a throwaway tree with fresh bindings for the
introspector and reads back only `typedef.json` (`typescript-sdk.dang:1034` —
"this tree is a throwaway"). C keeps that rule, so a fresh clone generates in
one pass; nothing is skipped-then-repaired.

Correcting round 1: an unresolvable *signature* type does not silently become
`any` — the introspector raises `IntrospectionError`
(`introspector/dagger_module/module.ts:334`). Body imports are untyped by the
scan, and a signature cannot name a foreign module's type anyway
(`core/module.go:1373`). Loud either way.

### 2.2 When `npm install` is enough

The entrypoint recipe bakes its mount from `package.json` at generate time —
`withMountedDirectory("node_modules/@dagger.io", workspace.directory("/<dir>"))`,
host `node_modules` excluded. Two cases:

- the directory is already in the recipe → installing any package already
  generated there is **live**, no regenerate;
- the scope's *first* client from a directory, or a registry client → the
  mount/install is not baked yet → `dagger call` fails until `dagger generate`.

So: one regenerate after the first install from a given directory.
`requireGenerated` should diff the manifest's `file:` clients against the baked
mounts and say *"run dagger generate"* instead of *"module not found"*.

### 2.3–4 The graph moves to where npm users expect it

Today `clients` on the consuming scope is the record; `list`/`rm` read it.
Under C, removing a package someone imports fails in the consumer, at typecheck
or import — the same fault, with the same loudness, as deleting any npm
dependency. Accepted as native. `rm` can grep the workspace's manifests for the
specifier and warn; `list` shows what is written, not who reads it.

### 2.6 Ordering: the graph already exists

`generateLocalDependencies` is **already gone** — removed from the schema on
manifest-v2, kept before only for pre-migration SDKs; the integration suite
asserts its absence (`generators_test.go:1699`). Its replacement is the engine's
scope graph: a `clients` entry naming a local module is an edge to that module's
scope, and scopes fold leaf-first (`sdkModuleGraphDependencies` →
`orderSDKModuleGraph`). C keeps the same entries, so it keeps the same edges.

The chain *A declares client X (git), some scope declares A's client, B installs
from it*: X needs no edge, A orders before its declarer, and B needs no edge at
all — its scan is staged and its recipe needs only a path. One
`dagger generate` from any state converges in one pass.

### 2.8 The one real gap: abort on first error

`runSDKModuleGeneratorGraph` returns on the first failing scope, so one
unfixably-broken module stops unrelated scopes from generating. Load failures
are already rescued — a broken module contributes no package until its own pass
repairs it — but a hard generation failure kills the fold. If "generate
everything that can be generated" is the bar, and it should be, the engine loop
needs continue-and-aggregate. Not C's fault: #56 behaves the same today.

## 3. Two SDKs, one directory: pruning, not naming

A `go.mod` and a `package.json` coexist in one directory fine. What does not:
each SDK writes its client directory **in full** and prunes what it did not
render — the TypeScript pass would delete Go's files, and vice versa. Either
each SDK prunes only its own file set (fragile), or each SDK picks its own
directory name — which they will anyway, since Go modules were never going to
live in npm's layout. Don't standardize the path; add the cheap
two-SDKs-same-directory error when a second SDK ships client generation.

## 4. Settled this round

- **Self clients are explicit**: `dagger mod client add .`, recorded as the
  resolved scope path (`"./modules/a"`, not a literal `"."`).
- **Legacy `[runtime]` modules**: out of scope; unified clients target
  entrypoint modules.
- **Resilience is the hard rule**: no scope's generation reads another scope's
  on-disk output. That is what makes "delete everything, `dagger generate`"
  converge, and it is already how the scan works.

## 5. UX walkthrough

One refinement surfaces here, worth stating first:

> **A client-only scope's directory *is* its client directory** — packages land
> directly in it, fully generated, nothing of the user's inside. A module scope
> nests them under `<module>/.dagger/clients/` to keep them out of the source
> tree. And the core `dagger/` package is not listed in `clients` — it is the
> floor every client directory carries, not a client.

### 5.1 Create a module

```
$ dagger module init typescript --name a --path modules/a
```

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "a"
```

```
modules/a/
  dagger-module.toml          [entrypoint]
  package.json                "@dagger.io/dagger": "file:./.dagger/clients/dagger"
  tsconfig.json
  src/index.ts                starter
  entrypoint/main.dang        recipe: mounts ./.dagger/clients at node_modules/@dagger.io
  __dagger.dispatch.ts
  loader.gen.ts
  .dagger/clients/
    dagger/                   @dagger.io/dagger — library + core bindings
```

Private by default: everything the module needs travels in its tree.
`import { dag, object, func } from "@dagger.io/dagger"` resolves on disk, before
anyone runs npm.

### 5.2 Add a client to the module

```
$ cd modules/a && dagger module client add github.com/my-mod/B
```

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "a"
clients = ["github.com/my-mod/B"]
```

```
modules/a/
  package.json                + "@dagger.io/b": "file:./.dagger/clients/b"
  .dagger/clients/
    dagger/
    b/                        @dagger.io/b
```

`import { b } from "@dagger.io/b"` — the recipe already mounts the directory,
so nothing else changes.

### 5.3 Add a self client

```
$ cd modules/a && dagger module client add .
```

```toml
clients = ["github.com/my-mod/B", "./modules/a"]    # resolved path, not "."
```

```
  .dagger/clients/
    dagger/  b/
    a/                        @dagger.io/a — the module's own bindings
```

`import { a } from "@dagger.io/a"` → `a(this.ws).foo()`. Never generated unless
asked for.

### 5.4 A client scope at `clients/`, shared

```
$ dagger module client add github.com/my-mod/B --scope ./clients
```

*(`--scope` is the one CLI addition: today scope detection needs a project
marker, and an empty directory has none.)*

```toml
[sdks.typescript.scopes."clients"]
clients = ["github.com/my-mod/B"]
```

```
clients/                      fully generated — the scope IS the client dir
  dagger/
  b/
  node_modules/@dagger.io/*   links, package -> sibling
```

**From your code** — no Dagger involvement, and no scope record for the consumer:

```
$ cd apps/web && npm install ../../clients/b
$ tsx main.ts                 import { b } from "@dagger.io/b"
```

**From a module** — the one-directory rule (§3.2 of take 2) means module `a`
cannot keep its private directory *and* install from `clients/`. Going shared is
wholesale: declare the module's clients there, install, regenerate.

```
$ dagger module client add ./modules/a --scope ./clients     # a's self client, now shared
$ cd modules/a && npm install ../../clients/b ../../clients/a
$ dagger generate
```

```toml
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "a"                    # no clients key — it writes nothing now

[sdks.typescript.scopes."clients"]
clients = ["github.com/my-mod/B", "./modules/a"]
```

```
clients/{dagger,b,a}/
modules/a/
  package.json                "@dagger.io/b": "file:../../clients/b", …
  (.dagger/clients/ pruned)
```

The `dagger generate` does three things: repoints `@dagger.io/dagger` at the
same directory (one-directory rule — core follows the clients), prunes the
now-unused private directory, and re-bakes the recipe's mount to `/clients`.
This is also §2.2's "first install from a new directory" case — the one install
that needs a generate after it.

## 6. What C needs

1. The staging rule, stated as policy (already true for the scan).
2. `requireGenerated` diffs the manifest's `file:` clients against the baked
   mounts, so a premature `npm install` fails with the right sentence.
3. Per-SDK client directory names.
4. Engine: continue-on-error in the generation fold — shared with every design.
5. Self clients explicit.
