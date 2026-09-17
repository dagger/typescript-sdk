# Unified clients, take 2: a client is written where it is declared

> Replaces the `clientRoot` idea in [`unified-clients-spec.md`](./unified-clients-spec.md).
> Sketch — layout and `dagger.toml` only.

## 0. What broke

`clientRoot` made `clients` a *wish* and the directory a *derivation*. Two module
scopes wanting `B` into the same root: `B`'s package exists because of two lines,
and deleting either one may or may not remove it. You cannot read the directory's
contents off the config, and nobody can answer "who owns this file".

Dropped.

## 1. The inversion

Two declarations. A scope makes **one** of them, never both.

| | means | new? |
| --- | --- | --- |
| `clients = [...]` | **these packages are written here.** Literally the contents of this scope's client directory | no — today's meaning, restored |
| `use = [...]` | **I resolve another scope's clients.** Writes nothing | yes |

That is the whole idea. `clients` stops being a want and goes back to being an
inventory: one author, one directory, no union, no ordering subtlety. `use` is a
symlink, expressed in config.

**Why never both:** each client directory carries its own `@dagger.io/dagger`.
Two directories reachable from one scope is two copies of the library, two
`globalConnection`s, and cross-client object passing breaks silently.

## 2. Layout: one rule

> A scope's clients live at `<scope>/.dagger/clients/`.

Same relative path under every scope, module or not. The shared case is just the
workspace root being a scope.

**Shared** — `.` declares, `modules/a` uses:

```
dagger.toml
.dagger/clients/
  dagger/                     @dagger.io/dagger
  b/                          @dagger.io/b
  a/                          @dagger.io/a          (A's self client)
modules/a/
  dagger-module.toml
  package.json                "@dagger.io/b": "file:../../.dagger/clients/b"
  src/index.ts
```

**Private** — `modules/a` declares, uses nothing:

```
dagger.toml
modules/a/
  dagger-module.toml
  package.json                "@dagger.io/b": "file:./.dagger/clients/b"
  src/index.ts
  .dagger/clients/
    dagger/  b/  a/
```

Private travels over git; shared does not, and is one copy for N consumers. That
is the only difference, and it is a placement choice, not a mode.

**Your question, answered.** Two modules, same target:

```toml
# they share it — declared once, where it lives
[sdks.typescript.scopes."."]
clients = ["github.com/my-mod/B"]

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
use = ["."]

[sdks.typescript.scopes."modules/b"]
is-module = true
name = "B"
use = ["."]
```

```toml
# or they each keep their own — two directories, two owners, no ambiguity
[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
clients = ["github.com/my-mod/B"]

[sdks.typescript.scopes."modules/b"]
is-module = true
name = "B"
clients = ["github.com/my-mod/B"]
```

Both are legible. Neither is a refcount.

## 3. Three spellings for `use`

### A — scope path *(recommended)*

```toml
[sdks.typescript.scopes."."]
clients = ["github.com/my-mod/B", "./modules/a"]

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
use = ["."]
```

One key, resolved like a scope key (config-dir-relative). A `use` target must be
a declared scope — so the config validates itself, and `dagger module client add`
has somewhere unambiguous to put things.

Single-element list rather than a string, purely so the "exactly one" rule is a
validation message and not a schema change if it ever relaxes.

### B — named client sets

```toml
[sdks.typescript.clients.shared]
path = ".dagger/clients"
modules = ["github.com/my-mod/B", "./modules/a"]

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
use = ["shared"]
```

A client set stops pretending to be a scope. Refactor-safe (move the directory,
keep the name), and it reads better when there are several. Costs a third table
and a second path-resolution rule.

Worth it only if multiple shared sets turn out to be common. They probably are
not.

### C — no link in `dagger.toml`

```toml
[sdks.typescript.scopes."."]
clients = ["github.com/my-mod/B"]

[sdks.typescript.scopes."modules/a"]
is-module = true
name = "A"
```

…and `modules/a/package.json` carries `"@dagger.io/b": "file:../../.dagger/clients/b"`,
written by `npm install` or by hand. Maximally native: the link lives in the file
the toolchain actually reads.

**Rejected**, for one concrete reason: generating `A` means scanning `A`'s source
for typedefs, and that source does `import { b } from "@dagger.io/b"`. The scan
needs `B`'s bindings. With no link in the config, generation cannot know that
before it parses `package.json` — so the engine would have to read a language
manifest to order its own work. `use` tells it in one line.

(`package.json` still decides one thing: a `@dagger.io/*` dep that is *not* a
workspace `file:` path came from a registry, so install it instead of mounting
it. That stays.)

## 4. `dagger module client add B`

| cwd is in a scope that | lands in | records |
| --- | --- | --- |
| uses another scope | that scope's directory | `clients` on the used scope |
| declares its own | its own directory | `clients` on this scope |
| neither | its own directory | `clients` on this scope |
| — with `--to <scope>` | that scope's directory | `clients` there, plus `use` here if missing |

`rm` is the mirror: remove from the declaring scope; if nothing else uses that
scope, `use` goes too.

## 5. What the engine passes

`generateScope` gains one argument — the used scope, already resolved:

```graphql
generateScope(
  ws: Workspace!,
  isModule: Boolean!,
  name: String!,
  clients: [ModuleSource!]!,   # what this scope writes  (empty when `use`)
  from: ClientScope,           # what this scope resolves (null when `clients`)
): Workspace!

type ClientScope { path: String!, clients: [ModuleSource!]! }
```

Exactly one of the two is populated, which is the §1 rule made unrepresentable
otherwise. `from.clients` is what the consumer needs to wire `package.json`, bake
the entrypoint mount, and stage-render for its typedef scan; `from.path` is where
it points.

Ordering falls out of what already exists: a scope that declares a client for a
local module depends on that module's scope, and a scope that `use`s another
depends on it. No unions, no cwd-sensitive planning, no reading `dagger.toml`
from inside the SDK.

## 6. Open

- **Self client.** A module's own package has to be somewhere. Shared: the used
  scope lists `"./modules/a"` (above). Private: implicit. Should the shared case
  be implicit too — the engine adds it when a module `use`s a scope?
- **`use` chains.** `a` uses `.`, `.` uses `vendor/`. Forbid, or flatten?
  Leaning forbid.
- **Two SDKs, one directory.** Nothing stops a TypeScript scope and a Go scope
  declaring clients at paths that overlap. Needs a check.
- **Legacy `[runtime]` modules** can only see their own directory, so they cannot
  `use`. Validation, with a message naming the reason.
- **Naming.** `use`, `from`, `clients-from`, `link`? `use` is short and reads
  right next to `clients`; it is also very generic.
