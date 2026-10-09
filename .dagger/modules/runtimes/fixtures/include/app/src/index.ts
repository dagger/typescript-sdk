import { readFileSync } from "node:fs"

import { check, func, object } from "@dagger.io/dagger"

import { greeting } from "../../shared/greet.js"

@object()
export class RuntimeInclude {
  /** Passes only if the sibling directory this module includes reached the call. */
  @func()
  @check()
  includeCheck(): void {
    if (greeting() !== "hello from shared") {
      throw new Error(`imported greeting() returned ${greeting()}`)
    }

    // Relative to the module directory, which is the container's workdir.
    const text = readFileSync("../shared/greeting.txt", "utf8").trim()
    if (text !== "hello from a file") {
      throw new Error(`read ../shared/greeting.txt as ${text}`)
    }
  }
}
