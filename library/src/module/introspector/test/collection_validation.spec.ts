import { describe, it } from "mocha"
import assert from "node:assert/strict"

import { scan } from "../index.js"
import { listFiles } from "../utils/files.js"

async function scanError(directory: string): Promise<string> {
  const files = await listFiles(
    new URL(`./testdata/${directory}`, import.meta.url).pathname,
  )

  try {
    await scan(files, directory)
  } catch (e) {
    return (e as Error).message
  }

  throw new Error(`scanning ${directory} should have failed`)
}

describe("Malformed collections", () => {
  const cases: { directory: string; expected: RegExp }[] = [
    {
      directory: "collectionNoKeys",
      expected: /collection Items at .*:\d+:\d+ requires a field decorated with keys\(\)\./,
    },
    {
      directory: "collectionMultipleKeys",
      expected: /collection Items at .* has multiple keys\(\) fields: names, paths\./,
    },
    {
      directory: "collectionKeysNotList",
      expected: /keys\(\) field names at .* must be a list\./,
    },
    {
      directory: "collectionNoGet",
      expected: /collection Items at .* requires a method decorated with get\(\)\./,
    },
    {
      directory: "collectionGetArity",
      expected: /get\(\) method lookup at .* must take exactly one argument\./,
    },
  ]

  for (const { directory, expected } of cases) {
    it(`is rejected by the introspector - ${directory}`, async () => {
      assert.match(await scanError(directory), expected)
    })
  }
})
