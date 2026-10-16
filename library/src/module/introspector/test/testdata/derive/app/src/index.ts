import { readFileSync } from "node:fs"

import { func, object } from "@dagger.io/dagger"

import { helper } from "./helper.js"
import { greet, Name } from "../../shared/greet.js"
import type { cfg } from "../../other/cfg.js"

@object()
export class App {
  @func()
  hello(name: Name): string {
    // A read no import can see — the blocker this approach has.
    const extra = readFileSync("../data/extra.txt", "utf8")

    return greet(name) + helper() + extra + String(cfg)
  }
}
