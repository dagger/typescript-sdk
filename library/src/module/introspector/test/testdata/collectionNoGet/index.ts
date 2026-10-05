import { collection, keys, func, object } from "../../../../../index.js"

@collection()
export class Items {
  @keys()
  names: string[] = []
}

@object()
export class CollectionNoGet {
  @func()
  items(): Items { return new Items() }
}
