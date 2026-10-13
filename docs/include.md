# Files outside the module directory

A module builds and runs with its own directory. Nothing else of the repository
reaches it: not the root, not a sibling package, not `.git`.

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

Each entry is a path relative to the module directory. A leading `!` excludes. A
directory entry covers the whole directory. Globs work (`../shared/*.ts`).

The paths are read at generation and written into the module's generated
entrypoint as its `resources`, so **re-run `dagger generate` after changing the
field**.

```typescript
import { greeting } from "../shared/greet.js"
```

## What that gets you

The engine reads `resources` while it loads the module and adds those paths to
the module's context — so they arrive in three places at once:

- **The module's calls see them.** The container a function runs in is built
  from the module context, with the module at its own subpath, so a relative
  import or a file read resolves exactly as it does in your checkout.
- **Changing one invalidates the cache.** The paths are part of the module
  digest, so they reach the cache key of every function call. Edit an included
  file and the next call runs again instead of replaying the old result.
- **`file:` dependencies resolve.** They are in the install context, so a
  `package.json` pointing at a package outside the module directory installs.

The module's own scan sees them too: a `@func` can take a type declared in an
included file and still come out with the right signature.

`.gitignore` applies to the context, and the engine drops every `.git` directory
from it once `resources` is non-empty — `.git/config` can hold credentials, and
`.git` changes on every fetch.

## What is refused

Generation fails rather than silently doing nothing, for a path that:

- is absolute,
- climbs past the workspace root,
- stays inside the module directory, which the module already builds with,
- matches nothing in the workspace.

The engine applies its own rules when it loads the module, and refuses a pattern
that leaves the **context root** — the nearest directory with a `.git` above the
module, which is not always the workspace root.

Two more cases have no `dagger.include`:

- **A module at the workspace root.** It already builds with every file in the
  workspace.
- **A `[runtime]` module.** Its files come from the module source the engine
  loads; `include` in its `dagger-module.toml` is the field for this.

## Requirements

`dagger.include` needs an engine that reads a `resources` member — dagger
v1.0.0-beta.17 or newer, which is what a generated module pins. A module that
declares nothing generates no `resources` member and loads on anything that can
load an entrypoint.
