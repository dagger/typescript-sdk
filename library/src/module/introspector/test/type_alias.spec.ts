import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { TypeDefKind } from "../../../core/client.gen.js"
import { scan } from "../index.js"
import { listFiles } from "../utils/files.js"

async function scanFixture(directory: string) {
  const files = await listFiles(
    new URL(`./testdata/${directory}`, import.meta.url).pathname,
  )

  return scan(files, directory)
}

describe("Type aliases", () => {
  it("rejects a union type alias", async () => {
    // It used to be registered as a scalar of its own name, which nothing
    // defines: the module then failed to load with `find mod type for ...`, or
    // — when the name matched a core type, as "Volume" does — resolved to that
    // type and failed at call time complaining about a volume address.
    try {
      await scanFixture("unionTypeAlias")
    } catch (e) {
      assert.match(
        (e as Error).message,
        /Unsupported union type alias 'Volume' in .*unionTypeAlias.*\. Declare an enum/,
      )
      return
    }

    throw new Error("scanning a union type alias should have failed")
  })

  it("keeps the intersection form a scalar, and boolean a boolean", async () => {
    // The two shapes the union check sits next to. A scalar is declared as a
    // string intersection — every core scalar is — and `boolean` is `true |
    // false` inside the compiler, so both reach that check and must not be
    // caught by it.
    const module = await scanFixture("scalarTypeAlias")
    const args = module.objects["ScalarTypeAlias"].methods["pull"].arguments

    assert.equal(args["ref"].type.kind, TypeDefKind.ScalarKind)
    assert.equal(args["force"].type.kind, TypeDefKind.BooleanKind)
  })
})
