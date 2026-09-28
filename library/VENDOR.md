# Vendoring record

`library/` is a copy of `sdk/typescript` from [dagger/dagger][upstream], not a
fork with an upstream remote. There is no merge base git can compute for you, so
this file is it: what was copied, from where, what was changed on the way in, and
how to do it again.

Keeping it accurate is the whole point. A re-vendor without a recorded delta list
is a blind overwrite, and the deltas below are small enough to lose without
noticing.

[upstream]: https://github.com/dagger/dagger/tree/main/sdk/typescript

## Current source

| | |
|---|---|
| Repository | `github.com/dagger/dagger` |
| Path | `sdk/typescript` |
| Tag | `v1.0.0-beta.11` |
| Commit | `a4e1e4ff663e5e51c2b96c2c0772f3d2f00cfb94` |

The tag is not incidental. This repo ships a library *built* for one engine
release, so the vendored sources, the bindings generated from them, the
committed `bundle/`, and the Go generator all move together or not at all.

## What was left behind

Deliberately not copied, and not to be copied by a future import:

- **The Go runtime module** (`runtime/`) — stays upstream; the engine owns it.
- **`dagger.json`** — a stray module config here would be discovered as a
  workspace module.
- **`src/core/client.gen.ts`** (upstream `src/api/client.gen.ts`) — regenerated
  by `.dagger/modules/packager`'s `library-bindings`, not vendored. It no longer
  reproduces upstream's file byte-for-byte: it sits at a different path and
  imports the runtime from `../session/context.js`. What still has to hold is
  that regenerating it here is a no-op.
- **Test and lint tooling, and the upstream changelog** — not part of the
  library.

## Local deltas

Everything else tracks upstream verbatim. These are the exceptions, and this
list is exhaustive — if an import produces a diff outside it, that diff is
either a new upstream change (fine) or a delta being dropped (not fine).

| File | Delta | Why |
|---|---|---|
| `package.json` | `scripts` removed | All pointed at things this repo does not have (mocha/eslint/tsx wiring, `../../docs/current_docs`). The packager owns how the bundle is built; see `.dagger/modules/packager/main.dang`. |
| `package.json` | `devDependencies` and `resolutions` trimmed | Kept to what the bundle build and the vendored test suite actually use: `rollup`, `rollup-plugin-dts`, the `@types` the declaration build needs, and `mocha`/`ts-node`/`@types/mocha` for `library-tests`. |
| `src/index.ts` | `entrypoint` no longer re-exported | It is the *dynamic* dispatcher, and re-exporting it drags the introspector — and so the TypeScript compiler — into `bundle/core.js`, which every module loads on every call. Importing the compiler costs about twice what the rest of the bundle costs, for something a generated module never calls: it carries a static `__dagger.entrypoint.ts` instead. `bundle/index.ts` drops the matching re-export. `packager:module-bundle-check` fails if the compiler reappears. |
| `package.json` | `version`, `author`, `license`, package metadata | **Tracks upstream.** Nothing here is published, so a local value is a delta that buys nothing and costs an import. Leave them alone. |
| `package.json` | `graphql` moved from `^17.0.1` to `^16.14.2` | `graphql-request@7.4.0` — the latest, and there is no v8 — peers on `graphql "14 - 16"`, so upstream's `^17.0.1` is a tree npm refuses outright with `ERESOLVE`. bun and yarn install it without a word, which is why it went unnoticed on both sides. Our own use of `graphql` is two `import type`s, so the range only has to satisfy the runtime dependency. Costs 182 KB off `bundle/core.js`. `packager:manifest-installs-check` holds it, and upstream has the same conflict — this is a fix waiting to go the other way. |
| `bun.lock` | Added (not upstream) | `yarn.lock` is a yarn v1 file that does not pin everything bun resolves; without `bun.lock` a rebuild days later drifts. Both are load-bearing — see `libraryBundle`'s doc comment. |
| `.mocharc.json` | Added (not upstream) | Runs the vendored introspector suite against `src/`. |
| `src/module/introspector/typescript_module/ast.ts` | `sdkPathAliases` feeds `paths` to `ts.createProgram` | Upstream scans a module from inside its own installed tree, where node_modules and a tsconfig already say what `@dagger.io/dagger` is. This SDK scans it in a bare container that has neither, and an unresolved SDK import does not fail the scan — the error type it yields still *prints* the written name, so the scan half-works and falls over only on signatures a name cannot carry (dagger/typescript-sdk#69). |
| `src/module/introspector/test/scan.spec.ts`, `test/testdata/sdkAlias/` | `sdkAlias` fixture and the case driving it | Guards the row above: the only fixture laid out as a module's source *beside* its generated SDK rather than as a flat directory, which is the layout the aliases exist for. |
| `bundle/` | Built here, committed | Not upstream at all. Produced by `packager:library-bundle`. |
| `src/` | **Relaid out into layers** | `common/` + `connect.ts` + `connectOpts.ts` + `provisioning/` became `session/`; `api/` became `core/`. The layering — session ← core ← module, telemetry to one side — is what lets these ship as separate packages later, and `packager:layering-check` holds it. See `design/package-split.md`. This is the largest delta in this table and the one an import is most likely to fight. |
| `src/core/connect.ts` | `connect` split out of `connection` | Upstream keeps both in `src/connect.ts`, but `connect` constructs a `Client` and so belongs with the bindings, while `connection` only establishes a session. Same split as the Go SDK's `dagger.Connect` vs `core.NewQuery`. |
| `src/session/shared.ts` | Added (not upstream) | `globalConnection` and `registry` are held on `globalThis` under `Symbol.for`, so two copies of the session package in one tree still share one instance. Measured: without it the decorator writes one registry and the dispatcher reads another, on npm, bun and deno alike. |
| `src/connection.ts` | Added; `connection` moved out of `src/connect.ts` | `connection` composes a session with the tracer's lifetime, so it belongs to neither layer — upstream has it in `src/connect.ts`, which makes `session` depend on `telemetry` and puts ~2.4 MB of OpenTelemetry SDK in every consumer's closure. What stays in `src/session/connect.ts` is `withSession`, the session half. `packager:layering-check` asserts that no layer reaches `telemetry`. |
| `src/module/entrypoint/`, `src/module/executor.ts` | **Deleted** | The dynamic dispatcher and everything only it reached — ~820 lines unreachable from both bundle entry points once `src/index.ts` stopped exporting `entrypoint`. `load()` survives as `src/module/introspector/load.ts`; `register.ts` and `introspection_entrypoint.ts` moved beside it. Still live upstream, so an import will keep offering them back. |

## What has to move with upstream

Two things here are duplicated outside `library/`, so an import that changes one
has to change the other:

- **Function registration.** `src/module/introspector/register.ts`'s
  `Register.addFunction` builds engine registration calls at runtime;
  `helpers/codegen/generator/typescript/templates/entrypoint_functions.go`'s
  `renderFunctionExpr` emits the same calls as static text for the generated
  entrypoint. Both are live. Nothing enforces that they agree, so a new
  decorator needs teaching to both, plus regenerated goldens.
- **The generator itself.** `helpers/codegen` is a port of upstream's TypeScript
  generator, not vendored, and moves on the same bumps. A three-way merge
  against the new tag is part of the import, not a follow-up.

## Re-vendoring

1. Pick the target tag. It must be the engine release this SDK is bumping to.
2. Check out `dagger/dagger` at that tag and copy `sdk/typescript` over
   `library/`, honoring **What was left behind** above.
3. Three-way merge the deltas in the table back in. Diffing the new upstream
   tree against the old one — not against `library/` — is what tells you whether
   upstream touched a file you had changed.
4. Regenerate, bindings first and bundle second, as **two separate runs**:

   ```shell
   dagger generate packager:library-bindings
   dagger generate packager:library-bundle
   ```

   `dagger generate packager` runs both against the same input snapshot, so the
   bundle gets built from the pre-regeneration bindings and comes out stale on
   the very change that moved them. Both are `@generate` functions, so the
   engine surfaces them as checks that fail when the committed output is stale —
   run the bundle again until it reports no changes.
5. Run `packager:library-tests` and the runtime checks. The runtimes group is the
   only thing that executes a generated module end to end.
6. Update the **Current source** table and any row of the delta table that moved.

Yarn-style scoped `resolutions` (`mocha/minimatch`, `mocha/serialize-javascript`)
were dropped on this import and are *not* restored: bun does not honor that
spelling — only the flat keys reach `bun.lock`'s `overrides` — and the lock
already resolves the versions they were reaching for.
