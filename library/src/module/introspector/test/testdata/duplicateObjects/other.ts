import { func, object } from "../../../../decorators.js"

@object()
export class DuplicateObjects {
  @func()
  two(): string {
    return "two"
  }
}
