import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { deriveResources } from "../imports.js"

const moduleRoot = new URL("./testdata/derive/app", import.meta.url).pathname

async function patterns(): Promise<string[]> {
  return (await deriveResources(moduleRoot, "src")).map((r) => r.pattern)
}

describe("Resources derived from imports", () => {
  it("finds what the source imports from outside the module", async () => {
    assert.deepEqual(await patterns(), [
      // tsconfig paths, pointing outside
      "../aliased",
      // package.json `file:` dependency
      "../lib",
      // `import type` — needed to scan, so needed all the same
      "../other",
      // a plain relative import
      "../shared",
    ])
  })

  it("leaves alone everything that resolves inside the module", async () => {
    const got = await patterns()

    // `./helper.js`, and the `@inside/*` tsconfig alias.
    assert.ok(!got.some((p) => p.includes("helper")))
    assert.ok(!got.some((p) => p.includes("src")))
    // The SDK's own generated `file:` dependency on clients/dagger: inside the
    // module, so deriving it would name the module's own tree as a resource.
    assert.ok(!got.some((p) => p.includes("clients")))
    // A registry dependency resolves in node_modules, which in a monorepo sits
    // at the repository root — outside the module and gitignored, so chasing it
    // would name a path the engine drops from the context anyway.
    assert.ok(!got.some((p) => p.includes("typescript")))
    // Node builtins are not paths.
    assert.ok(!got.some((p) => p.includes("fs")))
  })

  it("names what asked for each path", async () => {
    const byPattern = new Map(
      (await deriveResources(moduleRoot, "src")).map((r) => [
        r.pattern,
        r.origin,
      ]),
    )

    assert.match(byPattern.get("../shared") ?? "", /src\/index\.ts$/)
    assert.match(byPattern.get("../lib") ?? "", /package\.json \(dependencies\.@acme\/lib\)$/)
    assert.match(byPattern.get("../aliased") ?? "", /tsconfig\.json \(paths\.@aliased\/\*\)$/)
  })

  it("cannot see a file read with no import", async () => {
    // The limit of the whole approach, pinned rather than left implicit:
    // src/index.ts reads ../data/extra.txt with readFileSync, and nothing
    // static can tell. A module needing that still has to declare it.
    assert.ok(!(await patterns()).some((p) => p.includes("data")))
  })
})
