import assert from "assert"
import * as fs from "fs"
import * as path from "path"
import { fileURLToPath } from "url"

import { shared } from "../shared.js"

const here = path.dirname(fileURLToPath(import.meta.url))

describe("shared singletons", function () {
  it("returns the same instance for a repeated key", function () {
    const first = shared("spec:instance", () => ({ n: 1 }))
    const second = shared("spec:instance", () => ({ n: 2 }))

    assert.strictEqual(first, second)
    assert.strictEqual(second.n, 1, "the first creator wins")
  })

  it("keeps distinct keys distinct", function () {
    assert.notStrictEqual(
      shared("spec:a", () => ({})),
      shared("spec:b", () => ({})),
    )
  })

  // The case the whole mechanism exists for: two copies of the session module
  // in one process, which is what a resolver produces from conflicting ranges.
  // Without the globalThis slot each copy closes over its own module scope,
  // decorators register into one registry while the dispatcher reads another,
  // and nothing about the failure points at node_modules.
  //
  // The duplicate has to be a physical second file. A query-string import
  // (`./shared.js?copy`) is deduped by bun, which silently turns this into a
  // test of nothing — hence the precondition below.
  describe("with a duplicate copy of the module", function () {
    const duplicate = path.join(here, "shared.duplicate.ts")

    before(function () {
      fs.copyFileSync(path.join(here, "..", "shared.ts"), duplicate)
    })

    after(function () {
      fs.rmSync(duplicate, { force: true })
    })

    it("hands both copies the same instance", async function () {
      const copy = await import(`./shared.duplicate.js`)

      assert.notStrictEqual(
        copy.shared,
        shared,
        "precondition: the module must have been evaluated twice",
      )

      const fromOriginal = shared("spec:duplicate", () => ({ owner: "first" }))
      const fromCopy = copy.shared("spec:duplicate", () => ({
        owner: "second",
      }))

      assert.strictEqual(fromOriginal, fromCopy)
      assert.strictEqual(fromOriginal.owner, "first")
    })
  })
})
