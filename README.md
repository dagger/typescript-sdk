# typescript-sdk

A Dagger SDK for authoring Dagger modules and generating typed clients in
TypeScript.

This module implements the Dagger CLI 1.0 SDK contract: the engine calls its
`initModule` and `initClient` functions when you run `dagger module init` and
`dagger api client init`. It also exposes `targetRuntime` (`"typescript"`), so
modules it creates run on the built-in TypeScript runtime.

Backed by [`github.com/dagger/sdk-sdk/polyfill`](https://github.com/dagger/sdk-sdk/tree/main/polyfill).

## Install

Install the TypeScript SDK into your workspace:

```sh
dagger sdk install github.com/dagger/typescript-sdk
```

This registers the SDK under the name `typescript`, making it available to
`dagger module init typescript` and `dagger api client init typescript`.

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
dagger module init typescript my-module --runtime BUN
dagger module init typescript my-module --runtime DENO
```

`initModule` seeds `src/index.ts` plus runtime-specific config files:

- `node` / `bun` → `package.json`, `tsconfig.json`
- `deno` → `deno.json`

If `package.json`, `tsconfig.json`, or `deno.json` already exist at the target
path, init merges Dagger-required keys into them rather than overwriting — your
scripts, path aliases, unstable flags, and other custom settings are preserved.

The engine owns the module's `dagger.json`; the SDK only contributes the
template and config files above. Run `dagger generate` afterwards to produce the
generated SDK.

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

## Generate a typed client

Generate a TypeScript client bound to a module at a target path:

```sh
dagger api client init typescript ./lib/client .dagger/modules/api
```

The positional arguments are the output `<path>` and the target `<module>` (a
workspace-relative path or a canonical module ref). Pass `--dev` to generate
against the local development client instead of a pinned release:

```sh
dagger api client init typescript ./lib/client .dagger/modules/api --dev
```

The engine records the client (generator + directory) in workspace config and
the target `dagger.json` and generates the client files itself.

## Generate SDK files and clients

Regenerate every registered module and client in the workspace:

```sh
dagger generate
```

## Module management helpers

The SDK also exposes auxiliary functions for working with existing modules,
callable directly with `dagger call`.

### Configure an existing module

Read current configuration:

```sh
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config package-manager
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config base-image
```

Change configuration with `config set` — pass either flag, or both in a single
call. Each returns a `Changeset` so you confirm the diff before anything is
written:

```sh
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config set --package-manager pnpm@8.15.4
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config set --base-image node:23.2.0-alpine
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config set --package-manager pnpm@8.15.4 --base-image node:23.2.0-alpine
```

Unset stays as separate commands:

```sh
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config unset-package-manager
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module config unset-base-image
```

`package-manager` is only supported on Node modules; Bun and Deno bundle their
own and the SDK rejects the flag on those runtimes. `base-image` writes to
`deno.json` for Deno modules and to `package.json` otherwise — matching where
the engine reads it from.

### Discover and bulk-generate modules

```sh
# Generate a single module's SDK files
dagger -m github.com/dagger/typescript-sdk call \
    mod --path my-module generate

# Every TypeScript SDK module under the workspace
dagger -m github.com/dagger/typescript-sdk call modules path

# Generate every discovered module, skipping any with a skip marker
dagger -m github.com/dagger/typescript-sdk call generate-all
```

> `modules` and `generate-all` use legacy `dagger.json` scanning. For
> workspace-managed modules the engine owns the source of truth
> (`modules.<sdk>.as-sdk.modules`); prefer `dagger generate`.

## Skipping generation

To exclude a directory tree from `generate-all`, drop an empty
`.dagger-typescript-sdk-skip-generate` file at or above the module root. Useful
for fixtures, vendored modules, or anything you don't want regenerated in bulk.

```sh
touch some/fixture/.dagger-typescript-sdk-skip-generate
```

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface.
