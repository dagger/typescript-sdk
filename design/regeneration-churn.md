# Whole-file rewrites on an incremental `dagger generate`

> **Status: diagnosed. A fix is identified and prototyped, not yet implemented.**
> Reported 2026-09-14 while manually testing
> [typescript-sdk#52](https://github.com/dagger/typescript-sdk/pull/52); not
> caused by it. This note records the cause, two fixes that were measured and
> rejected, and the prototype that works.

## 1. The report

Editing a module's source and running `dagger generate` reports the core API
bindings and the module's config as whole-file additions. On `main`, adding or
removing one `@func()` — a 24-line edit:

```
__dagger.entrypoint.ts  +7      # correct
sdk/hi.gen.ts           +17     # correct
sdk/client.gen.ts       +18229  # the file is 18229 lines
package.json            +6      # the file is 6 lines
tsconfig.json           +9      # the file is 9 lines
```

Both directions, every run, indefinitely. The three whole-file entries are
byte-identical across the edit.

## 2. The cause

A changeset has two descriptions of itself, and `dagger generate` uses both:

1. **The paths it reports** come from `Changeset.ComputePaths`, which compares
   file content. These are correct — the three files are absent from them.
2. **The layer it applies and exports** is `Before.diff(After)`. This records
   **what was written**, not what changed.

`Workspace.withChanges` sizes its sparse read of the host from (1) and builds
the tree it diffs from (2). A file present only in (2) therefore has no
counterpart on the before side, and is reported as a whole new file.

So the question is why unchanged files are in (2). Two separate SDK behaviours
put them there:

- **Whole-directory writes.** `moduleFiles`, `withSelfBindings` and
  `clientScope` each hand the workspace a complete directory
  (`Workspace.withNewDirectory`). Every file in it is written, so every file is
  in the layer. This is what puts `package.json` and `tsconfig.json` there.
- **An intermediate write of `client.gen.ts`.** Generation runs two binding
  passes. The first makes the module loadable so its own client schema can be
  read; the second folds those self types back in. The two passes produce
  *different* `client.gen.ts` content, and both are written to the workspace the
  engine diffs. Only the second survives on disk, so the content is unchanged
  end to end — but the file was genuinely written with different bytes in
  between, so it is in the layer.

Measured directly: at the first pass, `client.gen.ts` has content digest
`sha256:7f5414…` while the copy on disk has `sha256:1b7f56…`. Both digests taken
with `excludeMetadata: true`, so this is a content difference, not metadata.

Why the layer is not content-filtered is an engine detail:
`Directory.diff` → `differFor` checks `fsdiff.GetUpperdir(lowerMnts, upperMnts)`,
and when it succeeds `HandleChanges` calls `overlayChanges`, which returns the
upper layer verbatim and compares nothing — `d.comparison` is only read by
`doubleWalkingChanges`. This was read from the engine source but not confirmed
to be the path taken by this particular diff. The SDK-side causes above are
confirmed by measurement and are sufficient to explain and fix the bug.

## 3. What it is not

Each of these was measured and ruled out:

- **Content.** Byte-identical across the edit (`shasum` on the exported files).
- **File metadata.** Identical on both sides: mode, uid, gid, size, and mtime to
  the nanosecond, on the host and as read back into the engine.
- **The SDK pin.** Moving it, editing a helper's source, and changing the
  codegen binary outright all report no changes. The engine caches an exec's
  output directory by content, so an identical directory is the same snapshot.
- **PR #52.** Reproduces on `main`.
- **A stale `clients/` directory**, and **git- versus path-resolved SDK module.**
  Both bisected out by making a clean workspace match a churning one, one
  attribute at a time.

## 4. Two fixes that do not work

**`Directory.withTimestamps(0)` on each generated tree.** Rejected: it makes the
addition direction three times worse.

| on a `@func()` edit | files | lines |
| --- | --- | --- |
| `main` | 5 | +18268 |
| with `withTimestamps(0)` | 9 | +134767 |

`Directory.WithTimestamps` allocates a snapshot on top of its parent and calls
`os.Chtimes` on every path, which copies the whole tree into that new layer. The
shipped bundle (`core.js`, +108029) then joins the churn.

This attempt was convincing for the wrong reasons: it makes the *removal*
direction clean, it passes an e2e check written against `Changeset.layer`, and
all 59 checks stay green. Only the addition direction fails.

**Normalizing timestamps inside the exec** (`find /out -exec touch -t
197001010000.00`), so they are stable without a copy-up layer. No effect at all:
byte-for-byte the same report as `main`. This rules out the timestamp theory.

## 5. The fix that works

Prototyped against the reproduction below. Both directions, two full cycles:
**2 files, ±24 lines** — exactly the files the edit reaches. Two changes, and
both are needed; either alone leaves part of the churn:

1. **Write generated files individually, skipping unchanged ones.** For each
   generated file, compare `File.digest(excludeMetadata: true)` against the same
   path in the workspace, and call `Workspace.withNewFile` only on a mismatch.
   `excludeMetadata: true` matters — the default digest includes metadata, and
   an unchanged file's digest differs without it.
2. **Keep the first binding pass out of the workspace the engine diffs.** Run it
   on a workspace used only to load the module and read its self schema, then
   write the final tree once onto the pre-pass workspace.

### What a production version still needs

The prototype is not shippable as written:

- It re-runs `moduleFiles` for the second pass, which re-runs the introspector
  scan of the user's source. That scan is the expensive half of generation.
  `withSelfBindings` exists today precisely to avoid it, and the final version
  should reuse the first pass's entrypoint and config rather than rebuild them.
- Stale binding removal changes from "replace the directory" to an explicit
  `Workspace.withoutFile` for each `*.gen.ts` no longer generated.
  `generatePrunesStaleBindingsCheck` covers this and must stay green.
- `Workspace.withNewFile` writes content only, so generated files lose their
  current permissions (`*.gen.ts` are `0600` today).
- `clientScope` still replaces the whole `clients/` directory, so a scope with
  `dualClients: true` will still churn. It needs the same treatment.

## 6. Reproducing it

The e2e harness **cannot** reproduce this. `Gen.module` compares two in-engine
workspaces, so the baseline is an overlay rather than files on disk, and that
diff compares content and is correctly quiet. A check written against
`Changeset.layer` in that harness passes on a broken fix.

Reproducing needs generated files really on disk:

```
dagger module init typescript --path .dagger/modules/<name>
dagger generate -y typescript-sdk:generate          # settle
# append a @func() to src/index.ts
dagger generate --no-apply typescript-sdk:generate  # main: 5 files, +18k
```

Then remove the function and repeat. Both directions must stay at two files.
Exercise both: the `withTimestamps` attempt passes if only the removal direction
is checked.

## 7. Unrelated trap found while investigating

Copying a workspace without its `.git` directory makes it load no modules at
all, silently. `dagger functions` prints "No functions found" and
`dagger generate` becomes a no-op that reports "no changes to apply". It will
not even restore a generated file that was deleted, and it exits 0 throughout.

Worth a separate issue against the engine: a workspace that cannot resolve its
modules should say so rather than report success.
