# typescript-sdk

A Dagger SDK for authoring Dagger modules and generating typed clients in
TypeScript.

The engine drives this module through the SDK-module interface
(dagger/dagger#13992): it records the projects this SDK manages in `dagger.toml`,
points the workspace working directory at one of them, and asks this module to
make it whole. Two functions carry that:

- `findClientRoot` answers with the project a location belongs to, or null when
  there is none.
- `generateScope` produces the complete project: the starter and the module
  config when it is new, and the generated bindings every time.

It has no module dependencies: workspace state, module lookup, and codegen all
go through the engine's native `Workspace` and `ModuleSource` APIs.

> [!IMPORTANT]
> This branch tracks dagger/dagger#13992, which is not released. The released
> engine (`v1.0.0-beta.11`) cannot run this module at all — see
> [Which engine runs what](#which-engine-runs-what).

## Scopes

A scope is one directory this SDK manages, recorded in `dagger.toml`:

```toml
[sdks.typescript]
module = "typescript-sdk"

# a module
[sdks.typescript.scopes."ci"]
is-module = true
name = "my-module"
clients = ["./lib/greeter"]

# a standalone client package
[sdks.typescript.scopes."lib/client"]
clients = ["github.com/some/module"]
```

A scope with `is-module` holds a Dagger module. Its `clients` are that module's
dependencies: each is recorded in the module's own `dagger-module.toml`, and its
types arrive as one `<dependency>.gen.ts` inside the module's `sdk/` directory.

A scope without `is-module` is a standalone typed client package. It binds
exactly one module; a second client goes in its own directory.

`findClientRoot` finds a project by walking up for the nearest `package.json`,
`deno.json`, or Dagger module config that names the TypeScript runtime. A
location inside `node_modules` resolves to the project, not to the installed
package it sits in.

## Install

From your workspace root:

```sh
dagger module install github.com/dagger/typescript-sdk
```

The engine recognizes the SDK interface and records the module as the
`typescript` SDK in `dagger.toml`. After install it is also available in
`dagger call` as `typescript-sdk`.

Commands that produce a `Changeset` print the diff and prompt for confirmation
before writing anything. Pass `--auto-apply` to skip the prompt.

## Create a new module

```sh
dagger module init typescript --name my-module
```

The engine records the module scope in `dagger.toml` and calls `generateScope`,
which renders the starter, writes `dagger-module.toml`, and generates the SDK
bindings in one step. Without `--path` the module lands in
`.dagger/modules/<name>/` beside `dagger.toml`; pass `--path` to put it
elsewhere:

```sh
dagger module init typescript --name my-module --path some/dir/my-module
```

The module is created with `src/index.ts` plus the config files its runtime
needs:

- `node` / `bun` → `package.json`, `tsconfig.json`
- `deno` → `deno.json`

Nothing already at the target path is removed. An existing `package.json`,
`tsconfig.json` or `deno.json` is merged into rather than overwritten, so it
keeps its scripts, path aliases, unstable flags and other settings; every other
file is left alone.

### Settings

The SDK's settings become typed flags on `dagger module init typescript`, and
the engine persists them on the scope:

```sh
dagger module init typescript --name my-module --template empty
dagger module init typescript --name my-module --runtime bun
dagger module init typescript --name my-module \
    --package-manager pnpm@8.15.4 \
    --base-image node:23.2.0-alpine
```

| Setting | Values | Default |
| --- | --- | --- |
| `template` | a directory name under `templates/`: `default` (a small working module) or `empty` (a bare `@object` class) | `default` |
| `runtime` | `node`, `bun`, `deno` | `node` |
| `package-manager` | a Node `name@version` pin, e.g. `pnpm@8.15.4`. Node only; bun and deno bundle their own | none |
| `base-image` | a container image the module builds on. Written to `deno.json` for deno, `package.json` otherwise | the SDK default |

They apply when a scope is created. An existing module's runtime is read back
from the config files it carries, and its package manager and base image are
edited with `mod config set` (below).

## Module clients

A module's dependencies are added as clients of its scope. A local target is
resolved against your current directory, so run these from the module's own
directory:

```sh
cd ci
dagger module client add typescript ../lib/greeter
dagger module client rm ../lib/greeter
dagger module client list
```

Each client is recorded in the module's `dagger-module.toml` and its types are
part of the module's generated bindings.

An empty client list is not read as "no dependencies": a module that keeps its
dependencies in its own manifest and records nothing in `dagger.toml` — every
module written before `dagger module client add` existed — is left alone. The
cost is that removing a scope's last client leaves that dependency in the
manifest, to be removed by hand.

## Generate a typed client

A client is a self-contained, scoped npm package in a scope of its own:

- `dagger.gen.ts` — the core API types
- `<module>.gen.ts` — the module it binds to
- `package.json` — its `@dagger.io/dagger` dependency pinned to the bound
  module's engine version
- `tsconfig.json`

The engine resolves a local target against your current directory, so enter the
scope first and name the module from there:

```sh
cd lib/client
dagger module client add typescript ../../.dagger/modules/api
```

It binds exactly one module and serves it through `Workspace.moduleSource`, so
it resolves from any plain client session rather than only from a module
runtime. If you point `@dagger.io/dagger` at a local bundle (e.g. `"./sdk"`),
regeneration preserves that instead of resetting it to the version pin.

Regeneration owns the `*.gen.ts` files and nothing else: bindings for a module
that is no longer bound are dropped, and your own files in the client directory
are left alone.

## Generate

```sh
dagger generate
```

The engine regenerates every recorded scope. It orders them so a scope that is a
client of another is generated first, which is how a module that imports a local
module always generates against up-to-date bindings.

Generation is anchored at your current directory, not the workspace root:
running it from a subdirectory regenerates the scopes at or under it.

## Module management helpers

The SDK also exposes auxiliary functions for working with existing modules,
callable directly with `dagger call`:

```sh
dagger call typescript-sdk <function> [flags]
```

### Configure an existing module

Read current configuration:

```sh
dagger call typescript-sdk mod --path my-module config package-manager
dagger call typescript-sdk mod --path my-module config base-image
```

Change configuration with `config set` — pass either flag, or both in a single
call. Each returns a `Changeset` so you confirm the diff before anything is
written:

```sh
dagger call typescript-sdk mod --path my-module config set --package-manager pnpm@8.15.4
dagger call typescript-sdk mod --path my-module config set --base-image node:23.2.0-alpine
dagger call typescript-sdk mod --path my-module config set \
    --package-manager pnpm@8.15.4 --base-image node:23.2.0-alpine
```

Unset stays as separate commands:

```sh
dagger call typescript-sdk mod --path my-module config unset-package-manager
dagger call typescript-sdk mod --path my-module config unset-base-image
```

`package-manager` is only supported on Node modules; bun and deno bundle their
own and the SDK rejects the flag on those runtimes. `base-image` writes to
`deno.json` for Deno modules and to `package.json` otherwise — matching where
the engine reads it from.

`--path` may point anywhere inside the module; `mod` walks up to the nearest
enclosing module config. Pass `--find-up=false` to address a module root
directly.

Config always resolves through the module's *source* directory, which the module
config's `source` field can move away from the module root — the layout `dagger
setup` migration produces, where the config lives in `.dagger/modules/<name>/`
and points back at pre-existing code:

```sh
# reads and writes ci/package.json, not .dagger/modules/my-module/package.json
dagger call typescript-sdk mod \
    --path .dagger/modules/my-module --find-up=false config package-manager
```

That split is also the one case `findClientRoot` cannot see through: standing in
`ci/` finds the TypeScript project there, not the module whose config points at
it. Address that module by path.

### Generate one module

```sh
dagger call typescript-sdk mod --path my-module generate
```

A module whose local dependency has never been generated cannot be generated on
its own: `sdk/` is not committed, so the dependency's schema is not loadable
until the dependency has been generated once. `dagger generate` handles the
ordering; generate the dependency first if you are driving one module by hand.

## Skipping generation

To exclude a directory tree from module generation, drop an empty
`.dagger-typescript-sdk-skip-generate` file at or above the module root. Useful
for fixtures, vendored modules, or anything you don't want regenerated.

```sh
touch some/fixture/.dagger-typescript-sdk-skip-generate
```

The marker holds an existing module only. A scope with no module config is being
created, and creating it half way would leave a module that cannot load.

## Which engine runs what

`dagger check` runs two groups against two different engines.

| Group | Released engine `v1.0.0-beta.11` | Engine built from dagger/dagger#13992 |
| --- | --- | --- |
| `e-2-e:*` except `sdk:helper-tests-check`, and `runtimes:*` | fail | pass |
| `e-2-e:sdk:helper-tests-check` | pass | pass |
| `engine-e-2-e:*` | pass — it builds the other engine | not run there |

The released engine does not have `moduleManifest`, which this module selects to
write a new module's manifest, and Dang infers a whole program on each call. So
every call into this SDK fails there, whatever the called function touches — a
check comparing a constant field of this module fails too. Only
`e-2-e:sdk:helper-tests-check`, which runs `go test` over `helpers/` and selects
nothing from this module, still passes. dagger/dang-sdk#13, dagger/go-sdk#37 and
dagger/python-sdk#25 are all in the same position.

Those checks are not disabled. `.dagger/modules/engine-e2e` builds an engine from
dagger/dagger#13992, runs it as a playground, mounts this checkout into it, and
runs them there:

- `engine-e-2-e:dev-sdk-check` drives the CLI: `dagger sdk list`, `dagger module
  init typescript`, and `dagger call` on the module it creates.
- `engine-e-2-e:checks-check` runs `dagger check "e-2-e:**" "runtimes:**"` inside
  the playground.

Both are pinned to one dagger/dagger commit, in two places: the `engine-dev`
dependency in `.dagger/modules/engine-e2e/dagger-module.toml` and `engineCommit`
in `.dagger/modules/engine-e2e/main.dang`. Bump them together.

The released-engine failures clear when an engine carrying dagger/dagger#13992
ships.

## Development

```sh
dagger check
```

Checks live in `.dagger/modules/e2e`, one file per SDK surface (lookup, scope,
config, generate, client), sharing the assertions in `util.dang` and driving the
fixture tree under `.dagger/modules/e2e/fixtures`. List them with `dagger check
-l`, or run one group with `dagger check "e-2-e:config:*"`.

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface, and
[`design/sdk-module-interface.md`](./design/sdk-module-interface.md) for why the
interface looks like this.
