> **Archived — round 2 of 3.** Superseded by
> [`../new-default-template.md`](../new-default-template.md). Kept for design
> history. This round reframed the choice around which first-class Dagger verb
> the default should teach (`publish` / `dagger check` / `dagger generate`) and
> proposed a Node-flavored skeleton. Later feedback dropped the Node/npm
> assumption (a TS module may not manage a JS/TS project) and deferred
> testing/linting to `dagger setup`, converging on a toolchain-neutral default.

# New default TypeScript init template

## Goal

Today `dagger module init typescript <name>` seeds an empty class:

```ts
import { object } from "@dagger.io/dagger"

@object()
export class MyModule {}
```

Correct, but it gives the user nothing to build on. They open the file to a
blank object and have to re-derive the boilerplate every module repeats: take
source from the workspace, build a container from it, run something, ship it.

We want `init` to drop the user into a *small but useful* module that already
shows that shape.

## Plan

1. Rename the current empty template `templates/minimal` → `templates/empty`
   (reachable via `--template empty` for a clean slate).
2. Add a useful `templates/default`, and make it the `initModule` default.

This revision reworks the three candidate styles around the round-1 feedback.
The interesting decision is no longer *"container builder vs pipeline"* — it's
**which first-class Dagger verb the default should teach**: `publish`,
`dagger check`, or `dagger generate`. The three styles below each headline one.

## Decisions locked from round 1

- **Constructor source**: `Directory` argument, `@argument({ defaultPath: "/",
  ignore: [...] })`. `/` = workspace root (`.` would resolve to the module dir,
  which isn't what "source of the workspace" means).
- **Ignore list**: `["**/node_modules", "**/.git", "**/dist", "**/.dagger"]`
  (dropped `**/build` — projects build into `dist`; added `.dagger`).
- **`baseImage`** is an object field decorated with `@func()`, and defaults to a
  **pinned** image — never `alpine:latest`.
- **No `withBaseImage`** — the base image comes from the constructor, so a
  chainable setter for it is redundant. The chainable mutation instead carries
  state the constructor *shouldn't* hold (credentials/secrets).
- **Template name**: `default`.

## What every style shares

The three styles differ only in their verbs; the skeleton is identical, and it
already satisfies the structural constraints (constructor + ignore, a `@func()`
field, and one meaningful chainable mutation):

```ts
@object()
export class {{ .ModuleName }} {
  /** Base image the build runs on. */
  @func()
  baseImage: string

  source: Directory

  constructor(
    @argument({
      defaultPath: "/",
      ignore: ["**/node_modules", "**/.git", "**/dist", "**/.dagger"],
    })
    source: Directory,
    baseImage = "node:22-slim",
  ) {
    this.source = source
    this.baseImage = baseImage
  }

  /** Development container with dependencies installed. */
  @func()
  buildEnv(): Container {
    return dag
      .container()
      .from(this.baseImage)
      .withMountedCache("/root/.npm", dag.cacheVolume("npm"))
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
      .withExec(["npm", "ci"])
  }
}
```

**Node-flavored on purpose.** The commands assume `npm`. This is the TypeScript
SDK — leaning into the JS/TS toolchain keeps the verbs *real* (`npm test`,
`npm run build`) instead of the `echo TODO` stub that made round-1 Style 3 weak.
The one-line escape hatch for other stacks: swap `baseImage` and the commands;
the object shape is unchanged. (Open question below: keep this, or ship a
generic `alpine` variant with a clearly-marked placeholder command.)

`{{ .ModuleName }}` is the only token `render-template` substitutes (camel-cased
class name).

---

## Style A — Publish pipeline  *(recommended)*

Headlines `publish`, with a `@check()` test gate. The base image is a
constructor arg (the `@func` field); the publish **tag is a function arg**;
registry **credentials arrive via the chainable** (they're sensitive and
optional, so they don't belong in the constructor).

```ts
import { dag, Directory, Container, Secret, object, func, check, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  /** Base image the build runs on. */
  @func()
  baseImage: string

  source: Directory
  username = ""
  secret?: Secret

  constructor(
    @argument({
      defaultPath: "/",
      ignore: ["**/node_modules", "**/.git", "**/dist", "**/.dagger"],
    })
    source: Directory,
    baseImage = "node:22-slim",
  ) {
    this.source = source
    this.baseImage = baseImage
  }

  /** Store registry credentials used when publishing. */
  @func()
  withRegistryAuth(username: string, secret: Secret): {{ .ModuleName }} {
    this.username = username
    this.secret = secret
    return this
  }

  /** Development container with dependencies installed. */
  @func()
  buildEnv(): Container {
    return dag
      .container()
      .from(this.baseImage)
      .withMountedCache("/root/.npm", dag.cacheVolume("npm"))
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
      .withExec(["npm", "ci"])
  }

  /** Run the test suite. */
  @func()
  @check()
  async test(): Promise<void> {
    await this.buildEnv().withExec(["npm", "test"]).sync()
  }

  /** Build and publish the image, returning its reference. */
  @func()
  async publish(address: string, tag = "latest"): Promise<string> {
    let ctr = this.buildEnv().withExec(["npm", "run", "build"])
    if (this.secret) {
      ctr = ctr.withRegistryAuth(address, this.username, this.secret)
    }
    return ctr.publish(`${address}:${tag}`)
  }
}
```

**Pros**
- Tells the full build → test → ship story, which is what sells Dagger.
- `withRegistryAuth` is a *genuinely* meaningful chainable: it adds a `Secret`
  the constructor shouldn't carry, and `publish` consumes it. Not
  constructor-redundant, not an API shadow.
- Runs `dagger check` (test gate) and produces a real published ref.

**Cons**
- Largest of the three (four verbs + a chainable).
- `publish` needs a registry the user supplies; the first thing a curious user
  runs is more likely `test` or `buildEnv`, which is fine, but `publish` won't
  work until they have somewhere to push.

---

## Style B — Checks module

Headlines `dagger check`. Two checks (`lint`, `test`) are the product; no
publish. The chainable injects a token for private dependencies (a real reason
checks need a `Secret`).

```ts
import { dag, Directory, Container, Secret, object, func, check, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  /** Base image the checks run on. */
  @func()
  baseImage: string

  source: Directory
  token?: Secret

  constructor(
    @argument({
      defaultPath: "/",
      ignore: ["**/node_modules", "**/.git", "**/dist", "**/.dagger"],
    })
    source: Directory,
    baseImage = "node:22-slim",
  ) {
    this.source = source
    this.baseImage = baseImage
  }

  /** Provide a token for installing private dependencies. */
  @func()
  withNpmToken(token: Secret): {{ .ModuleName }} {
    this.token = token
    return this
  }

  /** Development container with dependencies installed. */
  @func()
  buildEnv(): Container {
    let ctr = dag
      .container()
      .from(this.baseImage)
      .withMountedCache("/root/.npm", dag.cacheVolume("npm"))
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
    if (this.token) {
      ctr = ctr.withSecretVariable("NPM_TOKEN", this.token)
    }
    return ctr.withExec(["npm", "ci"])
  }

  /** Check the code style. */
  @func()
  @check()
  async lint(): Promise<void> {
    await this.buildEnv().withExec(["npm", "run", "lint"]).sync()
  }

  /** Run the test suite. */
  @func()
  @check()
  async test(): Promise<void> {
    await this.buildEnv().withExec(["npm", "test"]).sync()
  }
}
```

**Pros**
- Cleanest demonstration of the `@check()` decorator and `dagger check` (running
  multiple checks at once is the "aha").
- Every function runs out of the box with no external setup (no registry).
- Smallest, most self-contained of the three.

**Cons**
- `npm run lint` assumes a `lint` script exists; missing script → the check
  fails until the user wires one up (still better than a stub — it's a real
  command, not dead code).
- No shipping story; a user who wants build/publish has to add it.

---

## Style C — Generate module

Headlines `dagger generate`. Keeps a `@check()` test, and adds a `@generate()`
`format` that returns a `Changeset` — directly answering the round-1 "can we
show `@generate`?" question. `format` deliberately runs in a lean container
(no `npm ci`) so the changeset diffs only real source edits, not `node_modules`.

```ts
import { dag, Directory, Container, Changeset, object, func, check, generate, argument } from "@dagger.io/dagger"

@object()
export class {{ .ModuleName }} {
  /** Base image the tasks run on. */
  @func()
  baseImage: string

  source: Directory

  constructor(
    @argument({
      defaultPath: "/",
      ignore: ["**/node_modules", "**/.git", "**/dist", "**/.dagger"],
    })
    source: Directory,
    baseImage = "node:22-slim",
  ) {
    this.source = source
    this.baseImage = baseImage
  }

  /** Development container with dependencies installed. */
  @func()
  buildEnv(): Container {
    return dag
      .container()
      .from(this.baseImage)
      .withMountedCache("/root/.npm", dag.cacheVolume("npm"))
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
      .withExec(["npm", "ci"])
  }

  /** Run the test suite. */
  @func()
  @check()
  async test(): Promise<void> {
    await this.buildEnv().withExec(["npm", "test"]).sync()
  }

  /** Format the source and return the changes. */
  @func()
  @generate()
  format(): Changeset {
    return dag
      .container()
      .from(this.baseImage)
      .withDirectory("/app", this.source)
      .withWorkdir("/app")
      .withExec(["npx", "--yes", "prettier", "--write", "."])
      .directory("/app")
      .changes(this.source)
  }
}
```

**Pros**
- Shows both new first-class verbs (`@check` + `@generate`) in one small module.
- `format` is a real, useful generator: run it, review the diff, apply it — the
  exact `dagger generate` loop.
- The changeset-hygiene detail (lean container, diff vs `this.source`) teaches a
  non-obvious best practice by example.

**Cons**
- This style has **no meaningful chainable** — the structural "chainable
  mutation" constraint isn't met unless we graft one on (e.g. Style B's
  `withNpmToken`), which starts to bloat it.
- `format` overwrites files; a user who expected a read-only default may be
  surprised (mitigated by `dagger generate`'s review-before-apply flow).

---

## Comparison

| | A · Publish | B · Checks | C · Generate |
|---|---|---|---|
| Headline verb | `publish` | `dagger check` | `dagger generate` |
| Decorators shown | `@check` | `@check` ×2 | `@check` + `@generate` |
| `@func` field | `baseImage` | `baseImage` | `baseImage` |
| Chainable mutation | `withRegistryAuth` (Secret) | `withNpmToken` (Secret) | — (needs one grafted) |
| Runs with zero setup | test ✓, publish needs registry | ✓ all | test ✓, format ✓ |
| Size | largest | smallest | medium |
| Meets all structural constraints | ✓ | ✓ | ✗ (no chainable) |

## Recommendation

**Style A**, optionally grafting Style C's `@generate() format()` as a fifth
verb if we want the default to advertise `dagger generate` too.

Reasoning: A is the only one that tells the whole story (build → check → ship),
its `withRegistryAuth` chainable is the most defensible use of state (a `Secret`
the constructor shouldn't hold), and it exercises `@check` along the way. B is
the better pick if we'd rather the default be dead-simple and 100% runnable with
no registry. C's `@generate` is the most novel thing to show, but on its own it
lacks a natural chainable — it works best as an *addition* to A or B, not as the
standalone default.

Concretely, I'd ship **A**, and I'm inclined to add `format` from **C** so a
fresh module demonstrates `@check`, `@generate`, and `publish` — the three verbs
that make a module "first-class" — while staying under ~60 lines.

## Remaining open questions

1. **Node-flavored vs generic base.** Recommendation ships `node:22-slim` + npm
   commands. Alternative: `alpine:3.21` + a clearly-marked placeholder command,
   for repos that aren't Node. Node-flavored keeps the verbs real; generic is
   broader but reintroduces a placeholder. I lean Node-flavored + the one-line
   swap note. (Also: pin the patch — `node:22.11.0-slim` — for full reproducibility?)
2. **How many verbs is "small"?** A alone (4) vs A+`format` (5). Where's the line
   before it becomes something users trim?
3. **Does the default need a chainable at all?** The original constraint says
   yes. If we adopt C-style (generate-first) we'd have to add one artificially.
   Worth confirming the chainable is still a hard requirement vs a nice-to-have.
4. **`@up()`?** Not proposed here (services don't fit a generic default), but
   it's the one first-class verb none of the three show. Skip for the default?

## Mechanics / wiring

In [`typescript-sdk.dang`](../../typescript-sdk.dang):

- `initModule` default: `template: String! = "minimal"` → `"default"`.
- `renderedDefaultTemplate` special-cases the string `"minimal"` (runs
  `render-template` for `{{ .ModuleName }}` and `config-updator` for
  package.json/tsconfig/deno.json). Repoint that check at `"default"`, or
  generalize it so any template carrying a `.tmpl` file gets the same treatment.
- `templates/empty` (the renamed old template) keeps skipping that pipeline the
  way non-default templates do today.

No new template tokens are needed — `render-template` only substitutes
`{{ .ModuleName }}`, which every sketch uses for the class name.
