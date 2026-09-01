# Design: dev-engine clients via a user-provided local bundle

Status: proposed.

Companion to [`client-gen.md`](./client-gen.md). Covers **option (b)** from the
codegen review — letting a generated client work against an engine version that
isn't published to npm — **without any engine changes**.

## Problem

A generated client pins `@dagger.io/dagger` to the bound module's `engineVersion`
(`config-updater` → `npmVersion(engineVersion)`). Released engine → real npm
version, `npm install` works. Dev/unreleased engine (e.g. `1.0.0`, `1.0.0-0`,
`…-dev.…`) → not on npm, install fails.

## Approach — respect a user-vendored local `./sdk`

Rather than have the SDK ship/build a bundle or the engine expose one (too much
complication), let the **user** point `@dagger.io/dagger` at a local bundle in
their client dir and have codegen **respect** it:

```jsonc
// <client-dir>/package.json  (user-authored, for a dev engine)
{
  "dependencies": {
    "@dagger.io/dagger": "./sdk"   // or "file:./sdk"
  }
}
```

with the bundle vendored at `<client-dir>/sdk/` (core.js, core.d.ts, index.ts,
telemetry.ts + a small `sdk/package.json` — see "Obtaining the bundle").

The **only** SDK responsibility is: on (re)generation, do not clobber a
`@dagger.io/dagger` dependency the user has set to a local path. This mirrors
upstream's `Local` SDK-lib origin (`detectSDKLibOrigin`: `"@dagger.io/dagger" ==
"./sdk"` ⇒ Local), but with zero engine/bundle work on our side.

### What had to change (two small things) — ✅ both shipped

1. **`config-updater` must preserve a local `@dagger.io/dagger`.** ✅ Done —
   `isLocalDaggerRef` plus the guard in `updateClientPackageJSON`, covered by
   `TestUpdateClientPackageJSON_PreservesLocalDaggerRef`.

   It used to *always* overwrite the dep with the engine version:

   ```go
   packageJSON, _ = sjson.Set(packageJSON,
     "dependencies."+gjson.Escape(daggerLibPathAlias), npmVersion(engineVersion))
   ```

   Now: **if the existing value is a local reference** (starts with `.`,
   `file:`, `link:`, `workspace:`, or a git/URL scheme) keep it; **otherwise**
   set the engine-version pin. So:
   - fresh client / no dep / a version → engine-version pin (Remote, today's behavior);
   - user set `./sdk` / `file:./sdk` → **preserved** (local bundle).

   tsconfig/deno writers already use `setIfNotExists`-style semantics, so a
   user-provided `paths`/`imports` mapping for `@dagger.io/dagger` is already
   preserved; no change needed there beyond confirming they don't clobber a
   user's `./sdk` mapping.

2. **Generation must read the existing client-dir config.** ✅ Done —
   `existingClientConfig` in `typescript-sdk.dang`, passed by both
   `generateClient` and `generateAllClient`.

   They used to pass an **empty** directory as `existing` to `clientDirectory`,
   so `config-updater` read a non-existent `/existing/package.json` and started
   from `{}` — the user's `./sdk` dep was never seen. They now pass the real
   client-dir config, filtered from the workspace root so a not-yet-existing
   client dir falls back to empty.

   The vendored `sdk/` directory itself survives regeneration for free — though
   not because `withDirectory` is an overlay, as this originally claimed. The
   polyfill documents it as "add or replace", and the replace is real in the
   changeset's *after* tree. What makes this safe is that a client changeset is
   only ever applied to disk, and applying is additive. Confirmed by
   regenerating a client directory holding a vendored `sdk/core.js` and an
   unrelated `NOTES.md`: both survive, and `removedPaths` comes back empty.

   The distinction matters if this is reused elsewhere. Module generation stages
   its changeset into a workspace and reads it back, where the replace *does*
   drop unstaged files — see `module-gen.md`.

That's it. No new engine primitive, no bundle shipped by this repo, no new
generator modes.

## Obtaining the bundle (user side, out of SDK scope)

The SDK does not produce the bundle. For a dev engine the user vendors
`@dagger.io/dagger` into `<client-dir>/sdk/` — the same static library the engine
builds (`core.js` from `bun build ./src/index.ts --external=typescript
--target=node`, `core.d.ts` via the rollup dts config; see
`toolchains/engine-dev/build/sdk.go:158-177`) plus a thin `index.ts`
(`tsutils/client/index.ts`), `telemetry.ts`, and a minimal `sdk/package.json`
(`name: @dagger.io/dagger`, `main: ./core.js`, `types: ./core.d.ts`, `exports`
including `./telemetry`). We can document this / ship a helper script later, but
it is not required for the codegen change above.

## Open questions

- **Runtime resolution.** `"@dagger.io/dagger": "./sdk"` (or `file:./sdk`) makes
  `npm/yarn/bun install` link `./sdk` into `node_modules`, so a standalone client
  the user runs with `node`/`tsx` resolves the specifier at runtime. Confirm this
  round-trips (the `sdk/package.json` `main`/`exports` must be correct); tsconfig
  `paths` alone only covers typecheck, not runtime.
- **Local-ref detection.** Preserve on prefix `.` / `file:` / `link:`. Confirm
  that covers the shapes we want (`./sdk`, `file:./sdk`) and never matches a real
  version/range.
- **Should a version pin also be preserved when the user set a specific one?**
  Proposal: no — the SDK keeps owning the *version* pin (updates it to track the
  engine on regen); it only steps aside for **local** refs. Revisit if users want
  to pin a specific published version.

## Key references

- This repo: `helpers/config-updater/main.go` (`updateClientPackageJSON`,
  `npmVersion`, `setIfNotExists`), `typescript-sdk.dang` (`clientDirectory`,
  `generateClient`, `generateAllClient` — the `existing` argument),
  `design/client-gen.md`.
- Upstream `dagger/dagger`: `sdk/typescript/runtime/config.go`
  (`detectSDKLibOrigin`, the `./sdk` = Local rule), `tsutils/client/index.ts`,
  `toolchains/engine-dev/build/sdk.go:158-177` (how the bundle is built).
