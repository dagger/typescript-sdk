import { existsSync, readFileSync } from "node:fs"

import { check, func, object } from "@dagger.io/dagger"

@object()
export class RuntimeYarnBerry {
  /**
   * Passes only when the Berry yarn this module pins installed the
   * node_modules the call is running against.
   *
   * The install happens in another container, so what reaches here is the tree
   * it produced: Berry's node-modules linker leaves .yarn-state.yml, yarn 1
   * leaves .yarn-integrity, and neither records a version — which one wrote it
   * is the whole signal. The pinned 4.9.1 itself is asserted against the
   * generated recipe, by generate:entrypoint-installs-dependencies.
   */
  @func()
  @check()
  berryInstalledCheck(): void {
    if (existsSync("node_modules/.yarn-integrity")) {
      throw new Error(
        "node_modules was installed by yarn 1, which ignores the pinned yarn@4.9.1",
      )
    }
    if (!existsSync("node_modules/.yarn-state.yml")) {
      throw new Error(
        "no node_modules/.yarn-state.yml: the pinned Berry yarn did not install this tree",
      )
    }
    // Berry's default linker is PnP, which writes no node_modules at all for
    // the recipe to mount.
    const state = readFileSync("node_modules/.yarn-state.yml", "utf8")
    if (!state.includes("nmMode: classic")) {
      throw new Error(`.yarn-state.yml is not a node-modules install: ${state.slice(0, 200)}`)
    }
  }
}
