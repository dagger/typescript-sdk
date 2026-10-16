# Files outside the module directory

A module builds and runs with its own directory, plus whatever its code imports
from outside it. Nothing else of the repository reaches it: not the root, not an
unrelated sibling, not `.git`.

There is nothing to configure. An import that climbs out of the module directory
is picked up at generation and returned by the module's `sources` member:

```typescript
import { greeting } from "../shared/greet.js"
```

The directory the import names is what gets included — not just the file — so a
data file sitting beside the imported code comes along with it:

```typescript
readFileSync("../shared/version.txt", "utf8")   // works: ../shared is included
```

What is read, all of it the user's own code or config:

- a relative specifier that resolves outside the module directory,
- a `file:` or `link:` dependency in `package.json`,
- a `paths` target in `tsconfig.json` that points outside, resolved against
  `baseUrl` when it is set,
- an `imports` entry in `deno.json` that points outside — a Deno module has no
  package.json and no tsconfig, so its import map is the only alias it has.

A bare specifier with none of those is left alone: it resolves inside
`node_modules`, which in a monorepo sits at the repository root — outside the
module and gitignored, so naming it would name a path the engine drops anyway.

Also read, because each is a path the build needs and nothing else would
supply:

- a `tsconfig.json` `extends` pointing outside the module — tsx reads the chain
  at call time and dies with `File '../tsconfig.base.json' not found.`
- a `/// <reference path="../x.d.ts" />`.

## Reading a file is a different question

Resources make the module *build*. To *read* a file, take a `Workspace`
argument — the caller passes one, and it reaches anything in the workspace with
no resource involved:

```typescript
@func()
async extra(ws: Workspace): Promise<string> {
  return await ws.directory("/").file("data/extra.txt").contents()
}
```

That is the supported way, and it is why `readFileSync` on a path outside the
module is usually the wrong shape — it only works where derivation happened to
include the directory anyway:

```typescript
readFileSync("../shared/version.txt", "utf8")   // works: ../shared is imported
readFileSync("../data/extra.txt", "utf8")       // fails: nothing imports ../data
```

## Granularity

An import contributes the **directory** holding its target. A config entry
contributes exactly what it names: `"@shared/greet": "../shared/greet.ts"` in a
Deno import map derives the file, `"@shared/": "../shared/"` derives the
directory, and a `paths` target is truncated at its first glob so a nested
`@alias/deep/thing` still resolves.

## Requirements

A generated module needs **dagger v1.0.0-beta.18 or later**, which is what it
pins, whether or not it declares anything. Every module returns a `sources`
member, and declaring it is what gets a `types` and a `call` with no workspace
argument — so on an older engine the module does not load at all, and Dang's
interface check reports the workspace argument as missing.
