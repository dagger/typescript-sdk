# typescript-sdk

A Dagger SDK for authoring Dagger modules and generating typed clients in
TypeScript.

This module implements the Dagger CLI 1.0 SDK contract: the engine calls its
`initModule` and `initClient` functions when you run `dagger module init` and
`dagger api client init`, and its `@generate` functions when you run `dagger
generate`. It also exposes `targetRuntime` (`"typescript"`), so modules it
creates run on the built-in TypeScript runtime.

It has no module dependencies: workspace state, module discovery, and codegen
all go through the engine's native `Workspace` and `ModuleSource` APIs, which
requires an engine at `v1.0.0-beta.10` or newer.

## Install

Install the TypeScript SDK into your workspace:

```sh
dagger sdk install typescript
```

This installs `github.com/dagger/typescript-sdk` as `[modules.dagger-typescript-sdk]`
and registers it under the user-facing name `typescript`, so `dagger module init
typescript` and `dagger api client init typescript` dispatch to it.

Commands that produce a `Changeset` print the diff and prompt for confirmation
before writing anything. Pass `--auto-apply` to skip the prompt.

## Create a new module

Create a TypeScript SDK module under the default `.dagger/modules/<name>/`:

```sh
dagger module init typescript my-module
```

Pick a different location with `--path`:

```sh
dagger module init typescript my-module --path some/dir/my-module
```

Pick a runtime (`node` is the default; `bun` and `deno` are also supported):

```sh
dagger module init typescript my-module --runtime bun
dagger module init typescript my-module --runtime deno
```

`initModule` seeds `src/index.ts` plus runtime-specific config files:

- `node` / `bun` → `package.json`, `tsconfig.json`
- `deno` → `deno.json`

If `package.json`, `tsconfig.json`, or `deno.json` already exist at the target
path, init merges Dagger-required keys into them rather than overwriting — your
scripts, path aliases, unstable flags, and other custom settings are preserved.

The engine owns the module's config; the SDK only contributes the template and
config files above. Run `dagger generate` afterwards to produce the generated
SDK bindings.

Pick a starter with `--template` (`default` is a small working module, `empty`
is a bare `@object` class):

```sh
dagger module init typescript my-module --template empty
```

### Configure a module at creation

`module init` accepts configuration flags written into the module's
`package.json` (or `deno.json` for Deno modules):

```sh
dagger module init typescript my-module \
    --package-manager pnpm@8.15.4 \
    --base-image node:23.2.0-alpine
```

Both flags are optional. By default no `packageManager` field is written and no
base image override is set.

`--package-manager` accepts the Node-standard `name@version` syntax (e.g.
`npm@10.7.0`, `pnpm@8.15.4`, `yarn@1.22.22`). It is only valid with the Node
runtime; Bun and Deno bundle their own.

Run `dagger sdk module-options typescript` to list the flags the installed
version accepts.

## Generate a typed client

Generate a TypeScript client bound to a module at a target path:

```sh
dagger api client init typescript ./lib/client .dagger/modules/api
```

The positional arguments are the output `<path>` and the target `<module>` (a
workspace-relative path or a canonical module ref). Pass `--dev` to bind the
local development client instead of a pinned release:

```sh
dagger api client init typescript ./lib/client .dagger/modules/api --dev
```

The engine records the client (generator + directory) in workspace config and
the target module config; the SDK generates the files.

A client is a self-contained, scoped npm package holding:

- `dagger.gen.ts` — the core API types
- `<module>.gen.ts` — one per module in the bound module's closure
- `package.json`, `tsconfig.json` — pinned to the bound module's engine version

It binds exactly one module and serves it through `Workspace.moduleSource`, so
it resolves from any plain client session rather than only from a module
runtime. If you point `@dagger.io/dagger` at a local bundle (e.g. `"./sdk"`),
regeneration preserves that instead of resetting it to the version pin.

## Generate SDK files and clients

Regenerate every registered module and client in the workspace:

```sh
dagger generate
```

Generation is anchored at your current directory, not the workspace root:
running it from a subdirectory regenerates the project you're in and the
projects beneath it, and leaves the rest of the workspace alone.

Each module's local dependency closure is staged before its own codegen runs, so
a module that imports another local module always generates against up-to-date
bindings — including dependencies owned by a different SDK.

## Module management helpers

The SDK also exposes auxiliary functions for working with existing modules,
callable directly with `dagger call`. They are addressed by the workspace
install name (`dagger-typescript-sdk` by default):

```sh
dagger call dagger-typescript-sdk <function> [flags]
```

### Configure an existing module

Read current configuration:

```sh
dagger call dagger-typescript-sdk mod --path my-module config package-manager
dagger call dagger-typescript-sdk mod --path my-module config base-image
```

Change configuration with `config set` — pass either flag, or both in a single
call. Each returns a `Changeset` so you confirm the diff before anything is
written:

```sh
dagger call dagger-typescript-sdk mod --path my-module config set --package-manager pnpm@8.15.4
dagger call dagger-typescript-sdk mod --path my-module config set --base-image node:23.2.0-alpine
dagger call dagger-typescript-sdk mod --path my-module config set \
    --package-manager pnpm@8.15.4 --base-image node:23.2.0-alpine
```

Unset stays as separate commands:

```sh
dagger call dagger-typescript-sdk mod --path my-module config unset-package-manager
dagger call dagger-typescript-sdk mod --path my-module config unset-base-image
```

`package-manager` is only supported on Node modules; Bun and Deno bundle their
own and the SDK rejects the flag on those runtimes. `base-image` writes to
`deno.json` for Deno modules and to `package.json` otherwise — matching where
the engine reads it from.

`--path` may point anywhere inside the module; `mod` walks up to the nearest
enclosing module config. Pass `--find-up=false` to address a module root
directly.

Config always resolves through the module's *source* directory, which the
module config's `source` field can move away from the module root — the layout
`dagger setup` migration produces, where the config lives in
`.dagger/modules/<name>/` and points back at pre-existing code:

```sh
# reads and writes ci/package.json, not .dagger/modules/my-module/package.json
dagger call dagger-typescript-sdk mod \
    --path .dagger/modules/my-module --find-up=false config package-manager
```

### Discover and generate modules

```sh
# Generate a single module's SDK files
dagger call dagger-typescript-sdk mod --path my-module generate

# Every TypeScript SDK module in scope, as a cwd-relative path
dagger call dagger-typescript-sdk modules path

# Generate all of them; equivalent to what `dagger generate` runs
dagger call dagger-typescript-sdk generate-all-module
dagger call dagger-typescript-sdk generate-all-client
```

`modules` returns the modules this SDK manages — the
`[[modules.<sdk>.as-sdk.modules]]` entries the engine owns — narrowed to the
ones in scope from your current directory: every module at or below it, plus the
nearest enclosing one when the directory itself is not registered. A module is
registered by the directory holding its config, so a module whose `source`
points elsewhere is listed at its config path, not from inside its source tree.

## Skipping generation

To exclude a directory tree from module generation, drop an empty
`.dagger-typescript-sdk-skip-generate` file at or above the module root. Useful
for fixtures, vendored modules, or anything you don't want regenerated in bulk.

```sh
touch some/fixture/.dagger-typescript-sdk-skip-generate
```

The marker only affects module generation; registered clients are unaffected.

## Development

Run the end-to-end checks:

```sh
dagger check
```

Checks live in `.dagger/modules/e2e`, one file per SDK surface (lookup,
discovery, init, config, generate, client), sharing the assertions in
`util.dang` and driving the fixture tree under `.dagger/modules/e2e/fixtures`.
List them with `dagger check -l`, or run one group with
`dagger check "e-2-e:config:*"`.

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface.
