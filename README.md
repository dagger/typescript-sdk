# typescript-sdk

A Dagger SDK for authoring Dagger modules and generating typed clients in
TypeScript.

This module implements the Dagger CLI 1.0 SDK provider interface. The engine
records SDK scopes in `dagger.toml`, sets the workspace cwd to one, and asks
this module for that scope's complete desired state through `findClientRoot` and
`generateScope`. The module writes the files it owns; the engine owns the
workspace bookkeeping, and its builtin TypeScript runtime executes what we
write.

Workspace state, module discovery and codegen go through the engine's native
`Workspace` and `ModuleSource` APIs, which requires an engine at
`v1.0.0-beta.11` or newer. Module manifests are built with
[`dagger/sdk-helpers`](https://github.com/dagger/sdk-helpers), the one module
dependency.

## Install

Install the TypeScript SDK into your workspace:

```sh
dagger module install github.com/dagger/typescript-sdk
```

The engine inspects the installed module. Because it implements the complete
SDK-module interface, the engine also records it as an SDK, so `dagger module
init typescript` and `dagger module client add typescript` dispatch to it. List
what is registered with `dagger sdk list`.

Commands that write to the workspace print the diff and prompt for confirmation.
Pass `--auto-apply` to skip the prompt.

## Create a new module

Create a TypeScript SDK module under the default `.dagger/modules/<name>/`:

```sh
dagger module init typescript --name my-module
```

Pick a different location with `--path`:

```sh
dagger module init typescript --name my-module --path some/dir/my-module
```

Init writes the module's `dagger-module.toml`, seeds `src/index.ts` from a
template, and generates the module's own bindings, dispatch entrypoint and
runtime config in one step — there is no separate `dagger generate` afterwards.

The runtime decides which config files a module gets:

- `node` / `bun` → `package.json`, `tsconfig.json`
- `deno` → `deno.json`

Init never removes what is already at the target path. If `package.json`,
`tsconfig.json`, or `deno.json` are there, it merges Dagger-required keys into
them rather than overwriting — your scripts, path aliases, unstable flags, and
other custom settings are preserved — and any other file is left untouched.

A scope whose module is still configured by a pre-1.0 `dagger.json` is migrated
on its first generation: the manifest is rewritten as `dagger-module.toml`, with
`source`, `include` and `[[dependencies]]` carried over, and the `dagger.json` is
removed. The module's own source is generated, never scaffolded over.

### Settings

The SDK's settings become flags on `dagger module init` and `dagger module
client add`, and are persisted per scope in `dagger.toml`:

| Setting | Flag | Default |
| --- | --- | --- |
| `runtime` | `--runtime` | detected from the scope's config files, else `node` |
| `template` | `--template` | `default` (a small working module; `empty` is a bare `@object` class) |
| `packageManager` | `--package-manager` | unset |
| `baseImage` | `--base-image` | unset |

```sh
dagger module init typescript --name my-module --runtime bun
dagger module init typescript --name my-module --template empty
dagger module init typescript --name my-module \
    --package-manager pnpm@8.15.4 \
    --base-image node:23.2.0-alpine
```

`runtime` is detected rather than defaulted, so adopting an existing project, or
regenerating a module created before the setting existed, does not silently move
a Bun or Deno project onto Node. Setting it moves the scope to that runtime on
the next generation.

`--package-manager` accepts the Node-standard `name@version` syntax (e.g.
`npm@10.7.0`, `pnpm@8.15.4`, `yarn@1.22.22`). It is only valid with the Node
runtime; Bun and Deno bundle their own.

`--base-image` writes to `deno.json` for Deno modules and to `package.json`
otherwise — matching where the engine reads it from.

## Generate a typed client

Record a client for a module in the current scope:

```sh
dagger module client add typescript .dagger/modules/api
dagger module client add typescript github.com/acme/payments
```

There is no path argument: the SDK picks the layout. Which scope the client
lands in comes from `findClientRoot`, which answers with the directory of the
nearest `package.json`, `deno.json`, `deno.jsonc` or `tsconfig.json` above your
cwd — so a client belongs to the TypeScript project you are standing in. A
directory with no TypeScript project above it is not a scope this SDK can claim.

Where the generated package goes depends on what the scope is:

| Scope | Client output |
| --- | --- |
| A module | `clients/`, beside the module's generated `sdk/` |
| Your own project | `.dagger/clients/` |

One package per scope, not one per target: the core API is the bulk of a
generated client and every target in a scope shares it. The package holds

- `dagger.gen.ts` — the core API types
- `<module>.gen.ts` — one per recorded client target
- `package.json`, `tsconfig.json` — pinned to the engine release this SDK ships
  for, and named after the scope directory

A client-only package is self-contained and stands on its own. Install it
yourself — `"@dagger.io/<scope>-client": "file:./.dagger/clients"` plus your
package manager; the SDK never edits your own `package.json`. If you point
`@dagger.io/dagger` at a local bundle, regeneration preserves that instead of
resetting it to the version pin.

`clients` is the complete desired set, so `dagger module client rm` is just
regeneration without that target: its bindings go, and the last target leaving
takes the package with it.

## Regenerate

```sh
dagger generate
```

The engine reads the scope list, orders it so a module is generated before
anything holding a client for it, threads each result into the next, and calls
this SDK once per scope.

## Module management helpers

The SDK also exposes auxiliary functions for working with existing modules,
callable directly with `dagger call`, addressed by the workspace install name:

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

`--path` may point anywhere inside the module; `mod` walks up to the nearest
enclosing module config. Pass `--find-up=false` to address a module root
directly.

Config always resolves through the module's *source* directory, which the
module config's `source` field can move away from the module root — the layout
`dagger setup` migration produces, where the config lives in
`.dagger/modules/<name>/` and points back at pre-existing code:

```sh
# reads and writes ci/package.json, not .dagger/modules/my-module/package.json
dagger call typescript-sdk mod \
    --path .dagger/modules/my-module --find-up=false config package-manager
```

### Generate one module

```sh
dagger call typescript-sdk mod --path my-module generate
```

Addresses one module directly, for inspecting or repairing it in isolation. It
generates exactly the module asked for, against the workspace as it stands —
none of the scope ordering `dagger generate` does applies. A module configured
by a pre-1.0 `dagger.json` is refused: the engine's runtime regenerates those at
call time, so writing files here would only leave a second, differently
versioned copy behind.

## Development

Run the checks:

```sh
dagger check
```

`e-2-e:*` drives this SDK's functions the way the engine does, one file per
surface (lookup, discovery, init, config, generate, client), sharing the
assertions in `util.dang` and the fixture tree under
`.dagger/modules/e2e/fixtures`. `runtimes:*` generates a module per JavaScript
runtime and loads it. These checks, the packager checks, and fixture generation
run inside the pinned development engine. The outer workspace loads only the
engine harness; its inner workspace configuration lives in
`.dagger/modules/engine-e2e/workspace.toml`. `dagger generate` uses that same
engine to refresh the library artifacts and fixtures.

`engine-e-2-e:*` covers the half no dang check can reach: it builds an engine
from a pinned dagger/dagger commit, runs it as a playground with this checkout mounted,
and drives the real CLI through `sdk list`, `module init` with and without
settings, `call`, and the whole check suite. Provider validation is silent when
it fails — the engine simply never records `[sdks.typescript]` — so this is what
tells you the interface still matches. Bumping the engine commit means changing both
the `engine-dev` dependency in `.dagger/modules/engine-e2e/dagger-module.toml`
and `engineCommit` in `.dagger/modules/engine-e2e/main.dang`.

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface and
[`design/module-max.md`](./design/module-max.md) for why it is shaped this way.
