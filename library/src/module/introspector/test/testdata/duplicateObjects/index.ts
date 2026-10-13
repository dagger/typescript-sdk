import { func, object } from "../../../../decorators.js"

@object()
export class DuplicateObjects {
  @func()
  one(): string {
    return "one"
  }
}
