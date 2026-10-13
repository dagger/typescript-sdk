import { func, object } from "../../../../decorators.js"

// A set of allowed values written as a union. The introspector used to register
// this as a scalar named "Volume", which nothing defines.
type Volume = "loud" | "quiet"

@object()
export class UnionTypeAlias {
  @func()
  shout(volume: Volume): string {
    return volume
  }
}
