import { check, func, object } from "@dagger.io/dagger"

// Through the deno.json import map, which is a Deno module's only alias
// mechanism: nothing in this file names a path outside the module.
import { greet } from "@shared/greet"

@object()
export class RuntimeIncludeDeno {
  /** Passes only if the import map's target reached the call. */
  @func()
  @check()
  includeCheck(): void {
    if (greet("world") !== "hello world from ../shared") {
      throw new Error(`imported greet() returned ${greet("world")}`)
    }
  }
}
