import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { scan } from "../index.js"

/**
 * Scan one source directory of the shadowedCoreType fixture and fail unless the
 * declaration it holds was refused for shadowing a core type.
 *
 * Driven directly rather than through a flat fixture: a core name only exists
 * to be shadowed when the module's source sits beside its generated client, and
 * the source imports `@dagger.io/dagger`, which resolves only when the
 * generated files are declared as such.
 */
async function assertShadowRefusal(sourceDir: string, declaration: string) {
  const directory = new URL("./testdata/shadowedCoreType", import.meta.url)
    .pathname
  const generatedClientFiles = [`${directory}/sdk/client.gen.ts`]

  try {
    await scan(
      [`${directory}/${sourceDir}/index.ts`, ...generatedClientFiles],
      "shadowedCoreType",
      false,
      generatedClientFiles,
    )
  } catch (e) {
    assert.match(
      (e as Error).message,
      new RegExp(
        `${declaration} at .*shadowedCoreType.*index\\.ts:\\d+:\\d+ shadows the core Dagger type Volume\\.`,
      ),
    )

    return
  }

  throw new Error(`scanning ${declaration} should have failed`)
}

describe("Core type shadowing", () => {
  it("rejects a type alias shadowing a core type", async () => {
    // A union alias, which the type-alias branch would have refused on its own
    // — except the class lookup comes first and the generated `class Volume`
    // answered for the name, so generation passed and the module took a core
    // Volume.
    await assertShadowRefusal("src", "type alias Volume")
  })

  it("rejects a decorated class shadowing a core type", async () => {
    // This one was registered, as an object sharing a core type's name. Module
    // types are namespaced against other modules, not against core, so the core
    // Volume answered for it and its own fields could not be queried.
    await assertShadowRefusal("class", "class Volume")
  })

  it("rejects an undecorated class shadowing a core type", async () => {
    // Not part of the module's API by itself, and refused all the same: one
    // `@func` argument is all it takes for the core type to answer for it, and
    // the rule holds for the declaration rather than for a use of it.
    await assertShadowRefusal("plain", "class Volume")
  })
})
