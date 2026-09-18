# Collections

Requires the engine changes in [dagger/dagger#14221](https://github.com/dagger/dagger/pull/14221).

A collection has stored keys and a function that returns one item for a key.
The engine supplies `keys`, `get`, `list`, and `subset`. Other exposed
functions appear under `batch`.

```typescript
import { collection, delta, func, get, keys, object, CollectionDelta } from "@dagger.io/dagger"

@object()
class Item {
  @func() name: string = ""
}

@object()
@collection()
class Items {
  @func()
  @keys()
  paths: string[] = []

  @func()
  @delta()
  selection?: CollectionDelta

  @func()
  @get()
  item(key: string): Item {
    return Object.assign(new Item(), { name: key })
  }
}
```

The generated TypeScript and Dang entrypoints both carry collection metadata.
Self-call clients use the projected collection schema.

The engine fills the optional delta field before a module call. It compares the
current keys with the original keys. Copies preserve the internal base state.
A new object starts a new base. The internal state is not an exposed field.
