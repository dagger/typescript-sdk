# Whole-file rewrites on an incremental `dagger generate`

> **Status: fixed.** Reported 2026-09-14 while manually testing
> [typescript-sdk#52](https://github.com/dagger/typescript-sdk/pull/52); not
> caused by it. The fix is in this commit. Most of this note is the two fixes
> that were measured and rejected first, and how to test the next one.

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

## 5. The fix

Generation writes what it changed and nothing else. Three changes, all needed;
each one alone leaves part of the churn.

1. **File-by-file writes.** `moduleFiles` and `clientScope` walk the generated
   tree and write a file only when `File.digest(excludeMetadata: true)` differs
   from the same path in the workspace. `excludeMetadata: true` is doing real
   work — the default digest covers metadata, and two renders of the same bytes
   differ there every time. Written with `Workspace.withFile` rather than
   `withNewFile`, so what lands keeps the permissions it was rendered with.

   Deletions no longer come for free, so they are explicit: `prunedBindings`
   drops the `*.gen.ts` in `sdk/` this run does not generate, and
   `prunedClientFiles` drops anything in the client package we no longer render.

2. **The first binding pass never reaches the workspace the engine diffs.** It
   is staged with `stagedModule` onto a throwaway workspace, purely to make the
   module loadable so its own client schema can be read. `withSelfBindings` now
   returns a Directory — the first pass's tree with its bindings replaced —
   which `moduleFiles` writes once onto the pre-pass workspace. Reusing the
   first pass's entrypoint keeps the introspector scan running once, which is
   what `withSelfBindings` was always for.

3. **The manifest is kept only when it differs.** `withClientDependencies`
   re-emits `dagger-module.toml` every run and it comes out the same nearly
   every time. Everything the builder records lands in that one file, so
   comparing it is enough to say the run changed nothing, and the whole updated
   workspace is discarded when it matches. Without this the manifest replaces
   the bindings as the file reported on every edit — the whole-directory write
   used to mask it.

Measured on a module scope, adding and removing one `@func()`, three full
cycles:

| | files | lines |
| --- | --- | --- |
| before | 5 | +18268 |
| after | 2 | ±24 |

With `dualClients: true` it is 3 files, the third being the client package's own
bindings — which is correct, since the module's API changed.

### Two checks had to change

Both were reading a file out of `changes.after`, which worked only because
generation used to write every file whether or not it changed:

- `generateWorkspaceModuleCheck` asserts on the fixture's `package.json` and
  `tsconfig.json`, which a correct run leaves alone. It now reads the workspace
  with the changes applied.
- `generatePreservesManifestCheck` built its failure message from the manifest's
  contents. Dang evaluates the message whether or not the assertion fails, so it
  broke even while passing. Same fix.

## 6. Testing it

`generate-source-edit-is-scoped-check` guards it, and it asserts on
`Changeset.layer` rather than on the changeset's reported paths. That choice is
the whole reason the check works: the reported paths are diffed by content, so a
file rewritten with its own bytes is absent from them and a check reading them
passes on the bug. The layer records what was written, which is the thing being
fixed.

What the harness cannot show is the end-to-end symptom, because `Gen.scope`
compares two in-engine workspaces rather than files on disk. For that:

```
dagger module init typescript --path .dagger/modules/<name>
dagger generate -y typescript-sdk:generate          # settle
# append a @func() to src/index.ts
dagger generate --no-apply typescript-sdk:generate  # expect 2 files, ±24 lines
```

Then remove the function and repeat. **Run both directions.** The
`withTimestamps` attempt was clean on removal and three times worse on addition,
so a one-directional test would have passed it.

## 7. Unrelated trap found while investigating

Copying a workspace without its `.git` directory makes it load no modules at
all, silently. `dagger functions` prints "No functions found" and
`dagger generate` becomes a no-op that reports "no changes to apply". It will
not even restore a generated file that was deleted, and it exits 0 throughout.

Worth a separate issue against the engine: a workspace that cannot resolve its
modules should say so rather than report success.
