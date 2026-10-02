/* eslint-disable @typescript-eslint/no-explicit-any */
import assert from "assert"
import * as fs from "fs"
import { describe, it } from "mocha"
import path from "path"
import { fileURLToPath } from "url"

import { scan } from "../index.js"
import { listFiles } from "../utils/files.js"

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const rootDirectory = `${__dirname}/testdata`

type TestCase = {
  name: string
  directory: string
}

describe("scan by reference TypeScript", function () {
  const testCases: TestCase[] = [
    {
      name: "Should correctly scan a basic class with one method",
      directory: "helloWorld",
    },
    {
      name: "Should correctly resolve references",
      directory: "references",
    },
    {
      name: "Should correctly handle optional parameters",
      directory: "optionalParameter",
    },
    {
      name: "Should correctly handle context",
      directory: "context",
    },
    {
      name: "Should correctly scan scalar",
      directory: "scalar",
    },
    {
      name: "Should correctly scan enums",
      directory: "enums",
    },
    {
      name: "Should correctly scan legacy enum decorator",
      directory: "legacyEnumDecorator",
    },
    {
      name: "Should correctly scan list",
      directory: "list",
    },
    {
      name: "Should correctly scan variadic",
      directory: "variadic",
    },
    {
      name: "Should correctly scan void return",
      directory: "voidReturn",
    },
    {
      name: "Should correctly scan state",
      directory: "state",
    },
    {
      name: "Should correctly scan private method",
      directory: "privateMethod",
    },
    {
      name: "Should correctly scan object param",
      directory: "objectParam",
    },
    {
      name: "Should correctly scan multiple args",
      directory: "multiArgs",
    },
    {
      name: "Should correctly scan multiple objects as fields",
      directory: "multipleObjectsAsFields",
    },
    {
      name: "Should correctly scan multiple objects",
      directory: "multipleObjects",
    },
    {
      name: "Should correctly scan core enums",
      directory: "coreEnums",
    },
    {
      name: "Should correctly scan core enum defaults",
      directory: "coreEnumDefault",
    },
    {
      name: "Should correctly scan constructor",
      directory: "constructor",
    },
    {
      name: "Should correctly scan alias",
      directory: "alias",
    },
    {
      name: "Should correctly scan minimal",
      directory: "minimal",
    },
    {
      name: "Should correctly scan deprecated objects",
      directory: "deprecatedObject",
    },
    {
      name: "Should correctly scan deprecated functions",
      directory: "deprecatedFunction",
    },
    {
      name: "Should correctly scan deprecated arguments",
      directory: "deprecatedArgument",
    },
    {
      name: "Should correctly scan deprecated fields",
      directory: "deprecatedField",
    },
    {
      name: "Should correctly scan interfaces",
      directory: "interface",
    },
    {
      name: "Should correctly scan decorators",
      directory: "decorators",
    },
  ]

  for (const test of testCases) {
    it(`${test.name} - ${test.directory}`, async function () {
      this.timeout(60000)

      try {
        const files = await listFiles(`${rootDirectory}/${test.directory}`)
        const result = await scan(files, test.directory)
        const jsonResult = JSON.stringify(result, null, 2)
        const expected = fs.readFileSync(
          `${rootDirectory}/${test.directory}/expected.json`,
          "utf-8",
        )

        assert.deepStrictEqual(
          JSON.parse(jsonResult),
          JSON.parse(expected),
          `
Expected:
${expected}
Got:
${jsonResult}
        `,
        )
      } catch (e) {
        assert.fail(e as Error)
      }
    })
  }

  // Driven directly rather than through the loop above, which scans a flat
  // directory of sources: this fixture is a module's source *beside* its
  // generated SDK, the shape generation actually scans. There is no
  // node_modules and no tsconfig there, so every `@dagger.io/*` specifier
  // resolves only because the scan rebuilds the aliases itself — and an
  // unresolved one degrades into a type that prints as what the user wrote
  // rather than failing outright, so a flat fixture would never notice.
  it("Should resolve @dagger.io specifiers against the generated SDK - sdkAlias", async function () {
    this.timeout(60000)

    const directory = `${rootDirectory}/sdkAlias`
    const generatedClientFiles = [
      `${directory}/sdk/client.gen.ts`,
      `${directory}/sdk/dep.gen.ts`,
    ]

    try {
      const result = await scan(
        [`${directory}/src/index.ts`, ...generatedClientFiles],
        "sdkAlias",
        false,
        generatedClientFiles,
      )
      const jsonResult = JSON.stringify(result, null, 2)
      const expected = fs.readFileSync(`${directory}/expected.json`, "utf-8")

      assert.deepStrictEqual(
        JSON.parse(jsonResult),
        JSON.parse(expected),
        `
Expected:
${expected}
Got:
${jsonResult}
        `,
      )
    } catch (e) {
      assert.fail(e as Error)
    }
  })

  describe("Should throw error on invalid module", function () {
    it("Should throw an error when no files are provided", async function () {
      this.timeout(60000)

      try {
        await scan([], "")
        assert.fail("Should throw an error")
      } catch (e: any) {
        assert.equal(e.message, "no files to introspect found")
      }
    })

    it("Should throw an error if the module class has no decorators", async function () {
      this.timeout(60000)

      try {
        const files = await listFiles(`${rootDirectory}/noDecorators`)

        await scan(files, "noDecorators")
        assert.fail("Should throw an error")
      } catch (e: any) {
        assert.match(
          e.message,
          /is used by the module but not exposed with a dagger decorator/,
        )
      }
    })

    it("Should throw an error if a primitive type is used", async function () {
      this.timeout(60000)

      try {
        const files = await listFiles(`${rootDirectory}/primitives`)

        const f = await scan(files, "primitives")
        // Trigger the module resolution with a strigify
        JSON.stringify(f, null, 2)

        assert.fail("Should throw an error")
      } catch (e: any) {
        assert.equal(
          e.message,
          `Use of primitive 'String' type detected, please use 'string' instead.`,
        )
      }
    })
  })
})
