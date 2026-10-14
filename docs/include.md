# Files outside the module directory

A module builds and runs with its own directory, plus whatever its source
imports from outside it. Nothing else of the repository reaches it: not the
root, not an unrelated sibling, not `.git`.

## Imports are enough, usually

An import that climbs out of the module directory is picked up at generation and
written into the module's `resources`, so this needs no configuration at all:

```typescript
import { greeting } from "../shared/greet.js"
```

The directory the import names is what gets included — not just the file — so a
data file sitting beside the imported code comes along with it:

```typescript
readFileSync("../shared/version.txt", "utf8")   // works: ../shared is included
```

Three things are read, all of them the user's own code or config:

- a relative specifier that resolves outside the module directory,
- a `file:` or `link:` dependency in `package.json`,
- a `paths` target in `tsconfig.json` that points outside.

A bare specifier with none of those is left alone: it resolves inside
`node_modules`, which in a monorepo sits at the repository root — outside the
module and gitignored, so naming it would name a path the engine drops anyway.

## When you still have to say it

Static analysis cannot see a path that nothing imports:

```typescript
readFileSync("../data/extra.txt", "utf8")   // fails: nothing imports ../data
```

For that, list it in `dagger.include`, in the module's `package.json` — or its
`deno.json` under the Deno runtime, the same nested `dagger` object `baseImage`
lives in. Declared paths are added to the derived ones.

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

Both sets are read at generation and written into the module's generated
entrypoint's `sources` member, so **re-run `dagger generate` after changing an
import or the field**.

## What that gets you

The engine calls `sources` while it loads the module and merges what it returns
into the module's context — so the paths arrive in three places at once:

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
from it — `.git/config` can hold credentials, and `.git` changes on every fetch.

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

A generated module needs **dagger v1.0.0-beta.18 or later**, which is what it
pins, whether or not it declares anything. Every module returns a `sources`
member, and declaring it is what gets a `types` and a `call` with no workspace
argument — so on an older engine the module does not load at all, and Dang's
interface check reports the workspace argument as missing.
