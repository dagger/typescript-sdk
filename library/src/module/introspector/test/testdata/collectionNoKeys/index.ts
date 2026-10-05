import { collection, get, func, object } from "../../../../../index.js"

@object()
export class Item {
  constructor(public name: string) {}
}

@collection()
export class Items {
  @get()
  lookup(name: string): Item { return new Item(name) }
}

@object()
export class CollectionNoKeys {
  @func()
  items(): Items { return new Items() }
}
