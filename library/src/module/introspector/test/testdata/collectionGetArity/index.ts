import { collection, get, keys, func, object } from "../../../../../index.js"

@object()
export class Item {
  constructor(public name: string) {}
}

@collection()
export class Items {
  @keys()
  names: string[] = []

  @get()
  lookup(name: string, extra: string): Item { return new Item(name + extra) }
}

@object()
export class CollectionGetArity {
  @func()
  items(): Items { return new Items() }
}
