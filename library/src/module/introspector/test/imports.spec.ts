import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { deriveResources } from "../imports.js"

const moduleRoot = new URL("./testdata/derive/app", import.meta.url).pathname
const denoRoot = new URL("./testdata/deriveDeno/app", import.meta.url).pathname

async function patterns(root = moduleRoot): Promise<string[]> {
  return (await deriveResources(root, "src")).map((r) => r.pattern)
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
      // tsconfig `extends`: the file, not its directory, which would be the
      // whole parent of the module. tsx reads the chain at call time and dies
      // on a missing one.
      "../tsconfig.base.json",
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
    assert.match(byPattern.get("../tsconfig.base.json") ?? "", /tsconfig\.json \(extends\)$/)
  })

  it("reads a Deno module's import map", async () => {
    // A Deno module has no package.json and no tsconfig, so the import map is
    // its only alias mechanism — and `@shared/greet` is bare, so nothing in the
    // source itself points outside.
    // Sorted, and each named at the granularity the map named it: the
    // directory entry as a directory, the module entry as a file. The first
    // subsumes the second, which is redundant rather than wrong — the engine
    // takes a list of patterns.
    assert.deepEqual(await patterns(denoRoot), [
      "../shared",
      "../shared/greet.ts",
    ])

    const got = await patterns(denoRoot)
    // The module's own @dagger.io/dagger mapping stays inside it.
    assert.ok(!got.some((p) => p.includes("clients")))
    // `npm:` and `https:` targets are specifiers, not paths.
    assert.ok(!got.some((p) => p.includes("typescript")))
    assert.ok(!got.some((p) => p.includes("deno.land")))
  })

  it("cannot see a file read with no import", async () => {
    // The limit of the whole approach, pinned rather than left implicit:
    // src/index.ts reads ../data/extra.txt with readFileSync, and nothing
    // static can tell. A module needing that still has to declare it.
    assert.ok(!(await patterns()).some((p) => p.includes("data")))
  })
})
