import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { deriveResources } from "../imports.js"

const workspace = new URL("./testdata/derive", import.meta.url).pathname
const denoWorkspace = new URL("./testdata/deriveDeno", import.meta.url).pathname

async function patterns(
  root = workspace,
  modulePath = "app",
): Promise<string[]> {
  return (await deriveResources(root, modulePath, "src")).map((r) => r.pattern)
}

describe("Resources derived from imports", () => {
  it("finds what the source imports from outside the module", async () => {
    assert.deepEqual(await patterns(), [
      // tsconfig paths, pointing outside
      "../aliased",
      // Reached only through ../shared/greet.ts, which imports it: the second
      // hop, which the module would fail to compile without.
      "../deep",
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
      (await deriveResources(workspace, "app", "src")).map((r) => [
        r.pattern,
        r.origin,
      ]),
    )

    assert.match(byPattern.get("../shared") ?? "", /src\/index\.ts$/)
    // Attributed to the import that reached it, not to the module's own source.
    assert.match(byPattern.get("../deep") ?? "", /shared\/greet\.ts$/)
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
    assert.deepEqual(await patterns(denoWorkspace), [
      // Reached only through the mapped file, which imports it: a config
      // target is followed like any other import.
      "../deeper",
      "../shared",
      "../shared/greet.ts",
    ])

    const got = await patterns(denoWorkspace)
    // The module's own @dagger.io/dagger mapping stays inside it.
    assert.ok(!got.some((p) => p.includes("clients")))
    // `npm:` and `https:` targets are specifiers, not paths.
    assert.ok(!got.some((p) => p.includes("typescript")))
    assert.ok(!got.some((p) => p.includes("deno.land")))
  })

  it("resolves tsconfig paths against baseUrl when it is set", async () => {
    // Without reading baseUrl, `["x/*"]` would resolve inside the module and be
    // dropped as already-part-of-the-source. With it, the alias points one
    // level up, which is the whole reason someone sets it.
    const root = new URL("./testdata/deriveBaseUrl", import.meta.url).pathname

    assert.deepEqual(await patterns(root), ["../x"])
  })

  it("cannot see a file read with no import", async () => {
    // The limit of the whole approach, pinned rather than left implicit:
    // src/index.ts reads ../data/extra.txt with readFileSync, and nothing
    // static can tell. Reading a file is a Workspace argument's job — the
    // caller passes one, and it reaches anything in the workspace.
    assert.ok(!(await patterns()).some((p) => p.includes("data")))
  })
})
