# New default TypeScript init template

> **Status: implemented.** See `templates/default/`, the `initModule` wiring in
> `typescript-sdk.dang`, and the `e-2-e:template-check` test. Earlier iterations
> are kept under [`archives/`](./archives).

## Summary

When you run `dagger module init typescript <name>`, the SDK creates a new
module from a template. Today that template is an empty class, so every user
starts by writing the same boilerplate: take the workspace source and build a
container from it.

This proposal replaces the empty default with a small working module that
already does that. The current empty template is kept and renamed `empty`, so
`--template empty` still gives a blank start.

## Plan

1. Rename `templates/minimal` to `templates/empty`. It stays available as
   `--template empty`.
2. Add `templates/default` (below) and make it the template `init` uses by
   default.

## The template

```ts
import { dag, Workspace, Directory, Container, object, func } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  /** Image the build runs on. */
  @func()
  baseImageAddress: string

  source: Directory

  constructor(
    ws: Workspace,
    baseImageAddress = "alpine:3.21",
  ) {
    this.source = ws.directory("/", {
      exclude: ["**/node_modules", "**/.git", "**/dist", "**/.dagger"],
    })
    this.baseImageAddress = baseImageAddress
  }

  /** A container with the source mounted, ready to build on. */
  @func()
  container(): Container {
    return dag
      .container()
      .from(this.baseImageAddress)
      .withDirectory("/src", this.source)
      .withWorkdir("/src")
  }
}
```

## What each function does

- The **constructor** takes the `Workspace` (as `ws`) and the base image to
  build on. It loads the workspace root with `ws.directory("/", { exclude:
  [...] })`, leaving out directories that shouldn't go into the build. The
  parameter is named `ws`, not `workspace`, because a `workspace` constructor
  arg generates a `--workspace` flag that collides with the top-level
  `--workspace` flag and makes the module fail to run.
- **`container()`** returns a container with the source mounted. This is where
  the user adds their own build steps with `.withExec([...])`.

## Design decisions

**The source comes from the workspace.** The constructor takes a `Workspace` and
calls `ws.directory("/", { exclude: [...] })` to load the workspace root.
`exclude` skips `node_modules`, `.git`, `dist`, and `.dagger`; `include` is
available too if a module only cares about part of the tree.

**Base image is `alpine:3.21`.** It is neutral and pinned to a version rather
than `latest`. The default cannot assume the user's project is JavaScript or
TypeScript — someone may write a TypeScript module that builds a Python repo or
some infrastructure — so it does not default to a Node image. We can pin by
digest (`alpine:3.21@sha256:...`) if we want stricter reproducibility.

**The template stays minimal.** It gives the user a source directory and a
container to build on, and nothing else. The goal is to save time, not to teach:
there are no publish, check, or test functions to read through and delete.
`dagger setup` already suggests the modules that fit a project (`vitest`, `jest`,
`bun`, `biomejs`) when the user needs testing or linting.

## Implementation notes

Changes in [`typescript-sdk.dang`](../typescript-sdk.dang):

- Change the `initModule` default from `template: String! = "minimal"` to
  `"default"`.
- Generalize `renderedDefaultTemplate` (renamed `renderedTemplate`) so it
  renders any named template rather than special-casing `"minimal"`: it
  substitutes the class name with `render-template` and merges the runtime
  config files with `config-updator`. This runs for every built-in template, so
  `empty` also gets a valid `package.json`/`tsconfig.json` — it ships only
  `src/index.ts.tmpl`, the same as `default`.

`render-template` substitutes one token, `{{ .ModuleName }}` (the class name,
camel-cased). The template already uses it, so no new tokens are needed.
