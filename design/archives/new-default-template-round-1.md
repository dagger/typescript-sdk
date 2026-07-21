> **Archived — round 1 of 3.** Superseded by
> [`../new-default-template.md`](../new-default-template.md). Kept for design
> history. This round proposed three *object-shape* styles (build environment /
> container wrapper / CI pipeline) before feedback moved the design toward
> first-class Dagger verbs and a toolchain-neutral default.

# New default TypeScript init template

## Goal

Today `dagger module init typescript <name>` seeds an empty class:

```ts
import { object } from "@dagger.io/dagger"

@object()
export class MyModule {}
```

That is a correct starting point but gives the user nothing to build on. They
open the file to a blank object and have to remember how to accept a directory,
mount source, build a container, and chain state — the exact boilerplate almost
every module repeats.

This doc proposes replacing the default with a *small but useful* module so a
fresh `init` already shows the day-to-day shape of a Dagger module: take source
from the workspace, build a container from it, and chain state onto it.

## Plan at a glance

1. Rename the current empty template `templates/minimal` → `templates/empty`,
   and keep it reachable via `--template empty` for people who want a clean
   slate.
2. Add a new useful template and make it the `initModule` default.
3. Keep both wired through the existing `render-template` + `config-updator`
   pipeline (see [Mechanics](#mechanics--wiring)).

The rest of this doc is about **which** useful template to ship. Three styles
are proposed below — pick one (or a hybrid) and we iterate.

## Constraints (from the task)

- Realistic argument and function names — no `stringArg`/`foo`.
- Realistic use of state: **at least one object field** and **at least one
  chainable function that mutates object state**.
- **A constructor** taking a workspace / directory argument with a **default
  ignore** directive.
- Small — not overcomplicated.
- Not over-documented. If a user's reflex is to delete every doc comment
  because it's noise, we've failed. One short line per public surface, max.
- TypeScript template (not Dang).

## Shared conventions

All three styles share the same skeleton, so the choice is really about *what
the object does*, not its plumbing:

- Class name is rendered from the module name: `{{ .ModuleName }}` (the only
  placeholder `render-template` substitutes — camel-cased).
- Constructor captures workspace source via a `Directory` argument with
  `defaultPath` + `ignore`. `Directory` (not `Workspace`) is the idiomatic
  choice for a user module: `Workspace` is the engine/SDK-facing CLI-1.0 type,
  and user modules receive plain `Directory` arguments. Example from the docs'
  cookbook (`set-common-default-path`):

  ```ts
  constructor(
    @argument({ defaultPath: "." })
    source: Directory,
  ) {
    this.source = source
  }
  ```

- Base image defaults to `alpine:latest` so the template is **language-agnostic**
  and every function actually runs on a fresh `dagger call` with no external
  setup.

Two decisions apply to all three (see [Open questions](#open-questions)):
`defaultPath` = `"/"` (workspace root, matches "source of the workspace") vs
`"."` (module dir); and the exact default `ignore` list.

---

## Style 1 — Build environment  *(recommended)*

The object *is* a reproducible environment built from your source. State is the
source plus a swappable base image; `container()` assembles the env and `run()`
executes in it. This is the `buildEnv` idiom that shows up in nearly every real
module.

```ts
import { dag, Directory, Container, object, func, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  source: Directory
  baseImage: string

  constructor(
    @argument({ defaultPath: "/", ignore: ["**/node_modules", "**/.git", "**/dist"] })
    source: Directory,
    baseImage = "alpine:latest",
  ) {
    this.source = source
    this.baseImage = baseImage
  }

  /** Build the environment on a different base image. */
  @func()
  withBaseImage(image: string): {{ .ModuleName }} {
    this.baseImage = image
    return this
  }

  /** A container with the source mounted at /src. */
  @func()
  container(): Container {
    return dag
      .container()
      .from(this.baseImage)
      .withDirectory("/src", this.source)
      .withWorkdir("/src")
  }

  /** Run a command in the environment and return its output. */
  @func()
  async run(args: string[]): Promise<string> {
    return this.container().withExec(args).stdout()
  }
}
```

Constraints: field ✓ (`source`, `baseImage`) · chainable mutation ✓
(`withBaseImage`) · constructor + dir + ignore ✓ · 3 funcs, ~30 lines ✓ · one
line of doc each ✓.

**Pros**
- Smallest module that still exercises all four pillars (source+ignore, field,
  chaining, container build).
- Language-agnostic — nothing assumes npm/apk, so it's never "wrong" for a repo.
- Every function runs out of the box, no credentials or project-specific commands.
- `container()` returns a core `Container` other modules can keep chaining on.

**Cons**
- `withBaseImage` chaining is idiomatic but mild — the base image could just be
  a per-call arg; it's state mainly to demonstrate the pattern.
- `run(args)` is generic; it teaches the mechanism but not a concrete verb like
  "build" or "test".

---

## Style 2 — Container wrapper

State *is* an evolving `Container`. The constructor seeds it (base + source),
and each chainable method layers onto the held container. Mirrors the core
Container API, so it feels immediately familiar to Dagger users.

```ts
import { dag, Directory, Container, object, func, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  ctr: Container

  constructor(
    @argument({ defaultPath: "/", ignore: ["**/node_modules", "**/.git", "**/dist"] })
    source: Directory,
    base = "alpine:latest",
  ) {
    this.ctr = dag
      .container()
      .from(base)
      .withDirectory("/src", source)
      .withWorkdir("/src")
  }

  /** Install a package with apk. */
  @func()
  withPackage(name: string): {{ .ModuleName }} {
    this.ctr = this.ctr.withExec(["apk", "add", "--no-cache", name])
    return this
  }

  /** Set an environment variable. */
  @func()
  withEnvVariable(name: string, value: string): {{ .ModuleName }} {
    this.ctr = this.ctr.withEnvVariable(name, value)
    return this
  }

  /** The assembled container. */
  @func()
  container(): Container {
    return this.ctr
  }
}
```

Constraints: field ✓ (`ctr`) · chainable mutation ✓ (`withPackage`,
`withEnvVariable`) · constructor + dir + ignore ✓ · small ✓ · light docs ✓.

**Pros**
- Most fluent of the three; the "wrap a container and keep extending it" mental
  model is exactly how Dagger users already think.
- The constructor does "build a container + add workspace source" in one place —
  a direct hit on the task's example use case.
- Exposing `ctr` lets other modules grab the built container.

**Cons**
- `withPackage` hardcodes `apk`, so it silently assumes an Alpine base. Swap the
  base to Debian and the method breaks — a subtle trap in a *starter* template.
- Holding a whole `Container` in state bloats the object's identity. The docs
  explicitly caution against over-stuffed state (weakens caching); a starter
  template arguably shouldn't model the anti-pattern.
- Reads as a thin re-wrap of `Container` — less obviously "useful" than a verb.

---

## Style 3 — CI pipeline (build → test → publish)

The object models an end-to-end delivery. Source in the constructor, a mutable
`tag`, and the three verbs that sell Dagger. The most "complete" skeleton for a
real CI module.

```ts
import { dag, Directory, Container, object, func, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  source: Directory
  tag: string

  constructor(
    @argument({ defaultPath: "/", ignore: ["**/node_modules", "**/.git", "**/dist"] })
    source: Directory,
    tag = "latest",
  ) {
    this.source = source
    this.tag = tag
  }

  /** Set the image tag used by publish. */
  @func()
  withTag(tag: string): {{ .ModuleName }} {
    this.tag = tag
    return this
  }

  /** Build the application image from source. */
  @func()
  build(): Container {
    return dag
      .container()
      .from("alpine:latest")
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
  }

  /** Run the test suite and return its output. */
  @func()
  async test(): Promise<string> {
    return this.build().withExec(["sh", "-c", "echo 'add your tests here'"]).stdout()
  }

  /** Publish the built image to a registry. */
  @func()
  async publish(registry: string): Promise<string> {
    return this.build().publish(`${registry}:${this.tag}`)
  }
}
```

Constraints: field ✓ (`source`, `tag`) · chainable mutation ✓ (`withTag`, and
it genuinely affects `publish`) · constructor + dir + ignore ✓ · 4 funcs —
borderline on "small" · light docs ✓.

**Pros**
- Tells the whole story: build → test → publish, the arc that makes Dagger click.
- `withTag` is a *meaningful* chain — the mutated state changes `publish`'s result.
- Closest to a drop-in skeleton for an actual delivery module.

**Cons**
- `test()` has to be a stub (`echo 'add your tests here'`) — it can't know the
  project's test command. That's precisely the kind of placeholder users delete,
  which cuts against "make them gain time."
- `publish` needs a registry (and usually credentials); the first `dagger call`
  a curious user tries may fail confusingly on a brand-new module.
- Largest of the three; leans toward "overcomplicated" for a *default*.

---

## Comparison

| | Style 1 · Build env | Style 2 · Container wrapper | Style 3 · CI pipeline |
|---|---|---|---|
| Object field | `source`, `baseImage` | `ctr` | `source`, `tag` |
| Chainable mutation | `withBaseImage` | `withPackage`, `withEnvVariable` | `withTag` |
| Runs out of the box | ✓ all | ✓ all | build ✓, publish needs registry |
| Language-agnostic | ✓ | ✗ (`apk`) | ✓ |
| Size | smallest | small | largest |
| Models good state hygiene | ✓ | ✗ (Container in state) | ✓ |
| "Aha" factor | medium | medium | highest |

## Recommendation

Ship **Style 1**. It is the smallest template that still demonstrates every
required pillar, it's language-agnostic, and every function works immediately
with zero setup — which is what actually saves a new user time. Style 2's `apk`
trap and heavy-state model make it a poor thing to hand someone as *the*
example; Style 3's stub `test()` and credential-dependent `publish()` are the
kind of scaffolding people delete.

If we want more of Style 3's "aha," the cheap hybrid is to add a single concrete
verb to Style 1 (e.g. a `build(): Container` that runs one real step, or a
`publish(registry)`), while keeping `run()` for the generic case — without
importing the stub-`test()` problem.

## Mechanics / wiring

Changes needed in [`typescript-sdk.dang`](../../typescript-sdk.dang):

- `initModule`'s default: `template: String! = "minimal"` → the new default's
  directory name.
- `renderedDefaultTemplate` currently special-cases the string `"minimal"` (it
  runs `render-template` for `{{ .ModuleName }}` and `config-updator` for
  package.json/tsconfig/deno.json). That path must key off the *new* default's
  name (or be generalized so any template with a `.tmpl` file gets rendered).
- `templates/empty` (the renamed old template) should skip the render/config
  pipeline the same way non-default templates do today, or be given a trivial
  `index.ts.tmpl` so `{{ .ModuleName }}` still resolves — decide which.

No new placeholders are required: `render-template` only substitutes
`{{ .ModuleName }}`, which all three sketches already use for the class name.

## Open questions

1. **`defaultPath`: `"/"` vs `"."`** — `"/"` captures the workspace/context root
   ("source of the workspace", matches the task wording); `"."` captures the
   module dir. Since generated modules live under `.dagger/modules/<name>/`,
   `"/"` is likely what a CI-style module wants, but it then pulls in `.dagger`
   itself unless ignored.
2. **Default `ignore` list** — proposed `["**/node_modules", "**/.git",
   "**/dist"]`. Add `.dagger`? Add `**/build`? Keep it short so it's obviously
   editable.
3. **Name of the useful template** — `default`? Keep `minimal` (as
   small-but-useful) and use `empty` for the blank one? Go's SDK uses
   `minimal` = empty and `legacy` = the rich old default; we don't have to
   mirror that.
4. **Expose a field via `@func()`?** — the sketches keep fields as private
   state. Decorating one (e.g. `baseImage`/`tag`) surfaces it in
   `dagger functions`; worth it, or noise?
