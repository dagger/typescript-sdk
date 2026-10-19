import { func, object } from "@dagger.io/dagger"

// A decorated class named after a core type. It used to generate an object the
// schema had no room for: the core Volume answered for the name, so its own
// fields could not be queried.
@object()
export class Volume {
  @func()
  level(): string {
    return "loud"
  }
}
