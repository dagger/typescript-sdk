# Shared core: one `@dagger.io/dagger` the whole workspace points at

> The pivot the Go nominal-typing point forces. Refines
> [option C](./unified-clients-option-c.md); retires the one-directory rule from
> [take 2](./unified-clients-take2.md). Direction set with the team: **depend on
> the published core**, vendored at a fixed path until it exists (§2).

## 1. Why duplication is breakage, not waste

In TypeScript a duplicated core lib is wasted bytes: object passing is structural
(`value["_ctx"]`), so a `Container` from copy 1 crosses into copy 2 fine.

In Go it is a **type error**. `dagger.io/dagger.Container` is nominal — two copies
of the module are two distinct types, and `funcThatTakes(copy1.Container)` rejects
a `copy2.Container` at compile time. Same for Python's `isinstance`. The moment
core is duplicated, cross-client calls stop compiling.

So "one client directory per scope, each carrying its own core" (take 2 §3.2) was
a TypeScript-only escape. The real rule underneath it is stronger and
language-neutral:

> **One core per resolution context.** Not one directory — one *core*.

## 2. Core is the published library — vendored until it is

Not a setting. The **target** is a published `@dagger.io/dagger` that everything
depends on by version:

```jsonc
// every module, every client package — after publish
"dependencies": { "@dagger.io/dagger": "^1.0.0" }
```

It is not published yet, so until then we vendor it at **one fixed path per
SDK** and point a `file:` dep at it:

```jsonc
// today
"dependencies": { "@dagger.io/dagger": "file:../../.dagger/core/typescript" }
```

```
.dagger/core/typescript/       @dagger.io/dagger — the one vendored copy
```

Two properties make this a stopgap and not a design fork:

- **The swap is a no-op for generated code.** Every package already imports the
  bare specifier `@dagger.io/dagger`; only its `package.json` entry changes —
  `file:…/.dagger/core/typescript` → `^1.0.0` — and the vendored directory is
  deleted. No client regenerates, no import rewrites.
- **The `/typescript` suffix is the SDK namespace.** Go vendors
  `.dagger/core/go`, Python `.dagger/core/python`; each SDK owns its subdir, so
  the cross-SDK collision from option C §3 cannot happen here at all.

No `core = "scope"`, no per-scope choice. A module published over git depends on
the published core like everything else — until that exists, git consumption of an
entrypoint module stays the open item it already was (option C §2.7 /
`workspace-clients.md` §9), and we do **not** paper over it by bundling a private
core.

## 3. What this dissolves

**The one-directory rule is gone.** It existed only to prevent a second core in a
process. With core pinned to one location, a scope can install from as many client
directories as it likes — they all resolve the same `@dagger.io/dagger`, so one
session, one `Container` type, by construction. The singleton stops being a thing
you protect and becomes a thing the layout guarantees.

That collapses the awkward part of option C §5.4: a module no longer goes
"wholesale shared." It keeps private clients where it wants *and* installs from a
shared scope, because the only thing that ever had to be single — core — is.

## 4. Layout

```
.dagger/
  core/typescript/            @dagger.io/dagger        ONE vendored copy
  clients/                    a shared client scope (option C)
    b/                        @dagger.io/b   -> file:../core/typescript
modules/a/
  package.json                @dagger.io/dagger -> file:../../.dagger/core/typescript
                              @dagger.io/b       -> file:../../.dagger/clients/b
  .dagger/clients/
    a/                        @dagger.io/a   -> the same core   (self, still private)
```

Client packages shrink: they hold bindings and a dep on core, nothing more. The
3.9 MB bundle is vendored once now, and zero once core is published.

## 5. Version skew — decomposed, then dismissed

Your worry: two modules, two `engineVersion`s, needing two core views. It splits
into two cases, and neither survives contact.

**A git dependency does not share your core.** Its client (`@dagger.io/b`) is
generated *for you*, at your session's view — that is exactly what
`clientSchemaIntrospectionJSON` does. B's own source and B's own core live in B's
repo; you never compile them. So the classic skew (a dep pinned four releases
back, #56's `hello@v0.3.0`) never touches the shared core. It only shapes *b*'s
bindings, which are a separate package.

**Two local modules pinned apart is the only real case — and the engine already
makes it runtime-safe.** The compat view is applied *server-side at query time*
(`dagql SchemaForView`), keyed on the version each module declares. So a v0.9
module's calls are interpreted under v0.9 no matter what bindings shipped. The
*only* residue is compile-time: if a module's committed source was written against
an older core signature (`withDirectory(directory:)`) than the shared core renders
(`source:`), that module's source won't typecheck.

And that only happens if you deliberately pin one local module back and don't
regenerate it. Normal flow — bump the CLI, `dagger generate` — moves every local
module's `engineVersion` together and regenerates their sources against the one
core. Converged, no skew.

**With a single vendored core there is nothing to configure** — the workspace has
one `.dagger/core/typescript`, rendered at the session's version, and every local
module regenerates against it together. If a module is deliberately pinned back
and not regenerated, its source may not typecheck against the shared core; the fix
is to regenerate it, not to fork core. Once core is published, split versions
become the registry's problem (two ranges), which is where that concern belongs.

> Verdict: shared core is not just possible, it is *more correct* than duplicated
> core. One session has always implied one `Container`; this makes the type match
> the session. The skew case is unlikely and runtime-safe when it happens.

## 6. Open

- **Which version renders the vendored core?** #56's "newest local target wins"
  is the working answer; the definition is "the session's version." Fine as-is.
- **The published package is the real target.** Everything here is the shape that
  makes the eventual swap a specifier change and a directory deletion — nothing
  more. **It is not gated on `runtime-module.md`:** an entrypoint module already
  owns its container recipe (`entrypoint/main.dang` builds it) and already installs
  its dependencies (#57), so a published `@dagger.io/dagger` is just another
  installed dep, not a mount. What is left for publishing is the publish pipeline
  itself, one recipe tweak (install core rather than mount it under
  `node_modules/@dagger.io`), and emitting a version specifier instead of `file:`
  (`ModuleGeneratorConfig.PackagedClients`). None of it is runtime work.
- **Git consumption stays open**, unchanged: an entrypoint module consumed over
  git needs the published core to resolve (its `file:` vendored core does not
  travel, and re-vendoring it is the private-core we rejected). This is the one
  case genuinely waiting on publish.
