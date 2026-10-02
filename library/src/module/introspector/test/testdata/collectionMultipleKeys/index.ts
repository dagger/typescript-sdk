import { collection, get, keys, func, object } from "../../../../../index.js"

@object()
export class Item {
  constructor(public name: string) {}
}

@collection()
export class Items {
  @keys()
  names: string[] = []

  @keys()
  paths: string[] = []

  @get()
  lookup(name: string): Item { return new Item(name) }
}

@object()
export class CollectionMultipleKeys {
  @func()
  items(): Items { return new Items() }
}
