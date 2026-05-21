# typescript-sdk

A Dagger module for managing Dagger modules that use the TypeScript SDK.

The Dagger CLI ships without built-in module-management commands like
`init` or `develop`. Those operations live in SDK-specific modules like this
one, called through `dagger call`.

Backed by [`github.com/dagger/sdk-sdk/polyfill`](https://github.com/dagger/sdk-sdk/tree/main/polyfill).

## Install

From your workspace root:

```sh
dagger install github.com/dagger/typescript-sdk
```

After install, the module is available in `dagger call` as `typescript-sdk`.

Calls that return a `Changeset` will print the diff and prompt you to confirm
before writing anything to your workspace.

## Create a new module

Create a TypeScript SDK module under the nearest `.dagger/modules/<name>/`:

```sh
dagger call typescript-sdk init --name my-module
```

Pick a different location:

```sh
dagger call typescript-sdk init --name my-module --path some/dir/my-module
```

Pick a runtime (`node` is the default; `bun` and `deno` are also supported):

```sh
dagger call typescript-sdk init --name my-module --runtime BUN
dagger call typescript-sdk init --name my-module --runtime DENO
```

`init` seeds `src/index.ts` plus runtime-specific config files:

- `node` / `bun` → `package.json`, `tsconfig.json`
- `deno` → `deno.json`

If `package.json`, `tsconfig.json`, or `deno.json` already exist at the
target path, `init` merges Dagger-required keys into them rather than
overwriting — your scripts, path aliases, unstable flags, and other custom
settings are preserved.

`init` only seeds template and config files. Run `mod ... generate` to
produce the generated SDK.

## Generate SDK files

For a single module:

```sh
dagger call typescript-sdk mod --path my-module generate
```

For every TypeScript SDK module in the workspace (skipping any with a
`.dagger-typescript-sdk-skip-generate` marker at or above the module root):

```sh
dagger call typescript-sdk generate-all
```

## Manage dependencies

List:

```sh
dagger call typescript-sdk mod --path my-module deps list
```

Add (run `mod ... generate` after to refresh generated SDK files):

```sh
dagger call typescript-sdk mod --path my-module \
    deps add --source github.com/some/module
```

Add with a custom local name:

```sh
dagger call typescript-sdk mod --path my-module \
    deps add --source github.com/some/module --name alias
```

Remove by name or source:

```sh
dagger call typescript-sdk mod --path my-module deps remove --name alias
```

Update one remote dependency, or all of them:

```sh
dagger call typescript-sdk mod --path my-module deps update
dagger call typescript-sdk mod --path my-module deps update --name some-dep
```

## Manage the required engine version

```sh
# Read the version pinned in dagger.json
dagger call typescript-sdk mod --path my-module engine required

# Pin to a specific version
dagger call typescript-sdk mod --path my-module engine require --version 0.20.8

# Pin to the engine version you're currently running
dagger call typescript-sdk mod --path my-module engine require-current

# Pin to "latest"
dagger call typescript-sdk mod --path my-module engine require-latest
```

## Discover modules in a workspace

```sh
# Every TypeScript SDK module under the workspace
dagger call typescript-sdk modules path
```

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface.

## Skipping generation

To exclude a directory tree from `generate-all`, drop an empty
`.dagger-typescript-sdk-skip-generate` file at or above the module root.
Useful for fixtures, vendored modules, or anything you don't want
regenerated in bulk.

```sh
touch some/fixture/.dagger-typescript-sdk-skip-generate
```
