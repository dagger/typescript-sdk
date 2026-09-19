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

What a scope gets depends on what the scope is:

| Scope | Client output |
| --- | --- |
| A module | `sdk/` (the library) plus `clients/` (its per-module clients) |
| Your own project | `.dagger/clients/` — `sdk/` (the vendored library) plus flat per-module clients |

A module keeps its targets in a `clients/` directory beside the library:
every module — a dependency, a recorded target, the module itself — becomes its
own `clients/<module>.gen.ts` client with its own `dag` and entrypoint
functions, reached through its own specifier. `sdk/` stays the core-only
`@dagger.io/dagger` library:

```ts
import { dag, Container } from "@dagger.io/dagger" // core API (sdk/)
import { api } from "@dagger.io/api"               // a bound module (clients/api.gen.ts)

api().deploy(dag.container().from("alpine"))
```

The `@dagger.io/<module>` aliases are written into `tsconfig.json` (or
`deno.json`) at generation, so a client added inside a module is usable from it
straight away.

A standalone client scope renders the same client files as a module, packaged
as real npm packages:

```
.dagger/clients/
  dagger/          the vendored @dagger.io/dagger library
  <module>/        one package per target, named @dagger.io/<module>
```

Install what you use — a client's `file:` dependencies pull the library (and
any sibling it references) along:

```sh
npm install ./.dagger/clients/api
```

```ts
import { connection, dag } from "@dagger.io/dagger"
import { api } from "@dagger.io/api"
```

No path aliases, no remote `@dagger.io/dagger` dependency — plain package
resolution against the vendored, offline tree — and nothing of yours is
edited; you run the install. The same packages can later come from a registry
instead of a `file:` link, and a shared `.dagger/clients/` can back both a
module and your own code.

`clients` is the complete desired set, so `dagger module client rm` is just
regeneration without that target: its client goes, and the last target leaving
takes the scope with it.

## Regenerate

```sh
dagger generate
```

The engine reads the scope list, orders it so a module is generated before
anything holding a client for it, threads each result into the next, and calls
this SDK once per scope.

## Development

Run the checks:

```sh
dagger check
```

`e-2-e:*` drives this SDK's functions the way the engine does, one file per
surface (discovery, init, generate, client), sharing the
assertions in `util.dang` and the fixture tree under
`.dagger/modules/e2e/fixtures`. `runtimes:*` generates a module per JavaScript
runtime and loads it. List them with `dagger check -l`, or run one group with
`dagger check "e-2-e:discovery:*"`.

`engine-e-2-e:*` covers the half no dang check can reach: it builds an engine
from dagger/dagger#13992, runs it as a playground with this checkout mounted,
and drives the real CLI through `sdk list`, `module init` with and without
settings, `call`, and the whole check suite. Provider validation is silent when
it fails — the engine simply never records `[sdks.typescript]` — so this is what
tells you the interface still matches. Bumping the branch means changing both
the `engine-dev` dependency in `.dagger/modules/engine-e2e/dagger-module.toml`
and `engineCommit` in `.dagger/modules/engine-e2e/main.dang`.

See [`typescript-sdk.dang`](./typescript-sdk.dang) for the full type surface and
[`design/module-max.md`](./design/module-max.md) for why it is shaped this way.
