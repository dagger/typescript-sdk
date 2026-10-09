# Files outside the module directory

A module builds and runs with its own directory. Nothing else of the workspace
reaches it: not the repository root, not a sibling package, not `.git`.

To build with more, list it in `dagger.include`, in the module's `package.json`
— or its `deno.json` under the Deno runtime, the same nested `dagger` object
`baseImage` lives in.

```json
{
  "name": "web",
  "dagger": {
    "include": ["../shared", "!../shared/testdata"]
  }
}
```

Each entry is a path relative to the module directory. A leading `!` excludes.
Globs work (`../shared/*.ts`). The paths are read at generation and baked into
the module's generated entrypoint, so **re-run `dagger generate` after changing
the field**.

That makes them available in three places at once, which is the point: the
module is scanned against them, so an import reaching outside the module
directory resolves to what it will resolve to at run time; they are mounted
beside the module when a function runs, so a relative import or a file read
finds them; and they are in the install context, so a `file:` dependency
pointing outside the module directory resolves.

```typescript
import { greeting } from "../shared/greet.js"
```

`.gitignore` applies to the included paths — they are directories the module
does not own, so whatever the repository ignores in them is build output. The
module's own directory is read without that filter, since a module may well
ignore the generated `clients/` it cannot run without.

## What is refused

Generation fails rather than silently doing nothing, for a path that:

- is absolute,
- climbs past the workspace root — a module reads these from the workspace it
  is called in and can reach nothing above it,
- stays inside the module directory, which the module already builds with,
- matches nothing in the workspace.

Two more cases have no `dagger.include`:

- **A module at the workspace root.** It already builds with every file in the
  workspace.
- **A `[runtime]` module.** Its files come from the module source the engine
  loads; `include` in its `dagger-module.toml` is the field for this.
