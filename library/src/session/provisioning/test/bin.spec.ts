import assert from "assert"
import * as fs from "fs"
import * as os from "os"
import * as path from "path"

import { Bin } from "../bin.js"

/**
 * A stand-in for the CLI on PATH, as a shell script, so nothing here downloads
 * a binary or needs an engine. PATH is replaced while a test runs, so the
 * script touches its marker with a redirect rather than an external command.
 */
function fakeCLI(body: string): { dir: string; bin: string; marker: string } {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dagger-bin-spec-"))
  const bin = path.join(dir, "dagger")
  const marker = path.join(dir, "ran")
  fs.writeFileSync(bin, `#!/bin/sh\n: > ${marker}\n${body}\n`, { mode: 0o755 })
  return { dir, bin, marker }
}

const CONNECT_PARAMS = `echo '{"port":1234,"session_token":"spec-token"}'`

// An empty cliVersion makes downloadCLI fail before it reaches the network, so
// a test that falls through to the download fails fast instead of fetching.
const NO_DOWNLOAD = ""

function restore(key: string, value: string | undefined) {
  if (value === undefined) {
    delete process.env[key]
  } else {
    process.env[key] = value
  }
}

describe("Bin provisioning", function () {
  let savedPath: string | undefined
  let savedEngine: string | undefined

  beforeEach(function () {
    savedPath = process.env.PATH
    savedEngine = process.env.DAGGER_ENGINE

    if (process.platform === "win32") {
      // The stand-in is a shell script.
      this.skip()
    }
  })

  afterEach(function () {
    restore("PATH", savedPath)
    restore("DAGGER_ENGINE", savedEngine)
  })

  describe("choosing a CLI", function () {
    it("uses the one on PATH when the engine provides an address", async function () {
      const cli = fakeCLI(CONNECT_PARAMS)

      process.env.PATH = cli.dir
      process.env.DAGGER_ENGINE = "tcp://127.0.0.1:1234"

      const bin = new Bin(undefined, NO_DOWNLOAD)
      const client = await bin.Connect({})
      await bin.Close()

      assert.ok(client, "no client was returned")
      assert.ok(fs.existsSync(cli.marker), "the CLI on PATH was not the one run")
    })

    it("leaves the one on PATH alone without an engine address", async function () {
      const cli = fakeCLI(CONNECT_PARAMS)

      process.env.PATH = cli.dir
      delete process.env.DAGGER_ENGINE

      const bin = new Bin(undefined, NO_DOWNLOAD)
      await assert.rejects(() => bin.Connect({}))

      assert.ok(
        !fs.existsSync(cli.marker),
        "a CLI was taken from PATH with no engine address",
      )
    })
  })

  describe("when the CLI starts no session", function () {
    it("reports the binary, its exit code and its stderr", async function () {
      const cli = fakeCLI("echo 'no engine for you' >&2\nexit 3")

      const bin = new Bin(cli.bin, NO_DOWNLOAD)
      const err = await bin.Connect({}).then(
        () => undefined,
        (e: Error) => e,
      )

      assert.ok(err, "connecting to a CLI that exits should fail")
      // It used to read `port` off an undefined result and report that instead.
      assert.ok(
        !(err instanceof TypeError),
        `the failure surfaced as a TypeError: ${err.message}`,
      )
      assert.match(err.message, new RegExp(cli.bin))
      assert.match(err.message, /exited with code 3/)
      assert.match(err.message, /no engine for you/)
    })
  })
})
