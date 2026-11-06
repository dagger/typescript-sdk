// Wait until the registry serves every version named, because `npm publish`
// returning is not the same as a consumer being able to resolve it. This is what
// gates the dist-tag flip, which is the only user-visible step of a release.
//
// Reads the packument over HTTP rather than shelling out to `npm view`: a fetch
// here is a cold read by construction, with no client-side metadata cache to
// defeat. That matters because a stale packument is one of the three staleness
// sources and the only one polling fixes — see design/package-split.md.
//
// On failure it replays what each `npm publish` said, from --logs <dir>. That
// matters because publishing is tolerated so a release can resume, which makes
// it a successful span — and a successful span's output is not kept in the
// trace, so without this the reason a release failed is nowhere.
//
// Usage: node await-published.cjs [--logs <dir>] <registry> <name@version>...
const fs = require("fs")
const path = require("path")

const argv = process.argv.slice(2)
let logsDir = null
if (argv[0] === "--logs") {
  logsDir = argv[1]
  argv.splice(0, 2)
}
const registry = argv[0]
const specs = argv.slice(1)

const replayPublishLogs = () => {
  if (!logsDir) return
  let entries
  try {
    entries = fs.readdirSync(logsDir).sort()
  } catch {
    return
  }
  const said = entries.filter((name) => {
    const body = fs.readFileSync(path.join(logsDir, name), "utf8").trim()
    if (body === "") return false
    console.error(`--- npm publish said, in ${name}:`)
    console.error(body)
    return true
  })
  if (said.length === 0) {
    console.error(`--- npm publish said nothing at all (${logsDir} is empty)`)
  }
}

if (!registry || specs.length === 0) {
  console.error("usage: await-published.cjs <registry> <name@version>...")
  process.exit(1)
}

const timeoutMs = Number(process.env.AWAIT_TIMEOUT_MS ?? 120000)
const base = registry.replace(/\/+$/, "")

// Scoped names are keyed with the slash escaped; the leading `@` is not.
const url = (name, version) => `${base}/${name.replace("/", "%2f")}/${version}`

const parse = (spec) => {
  const at = spec.lastIndexOf("@")
  if (at <= 0) {
    console.error(`not a name@version: ${spec}`)
    process.exit(1)
  }
  return { name: spec.slice(0, at), version: spec.slice(at + 1) }
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

const main = async () => {
  const deadline = Date.now() + timeoutMs

  for (const { name, version } of specs.map(parse)) {
    let wait = 1000
    for (;;) {
      let ok = false
      try {
        const res = await fetch(url(name, version), { headers: { accept: "application/json" } })
        ok = res.ok
      } catch (err) {
        // A transport error is indistinguishable from "not there yet" from here,
        // and the deadline is what decides either way.
        ok = false
      }
      if (ok) {
        console.log(`serving ${name}@${version}`)
        break
      }
      if (Date.now() >= deadline) {
        console.error(`${name}@${version} is not served by ${base} after ${timeoutMs}ms`)
        // EOTP is the one to look for below: npm demands a one-time password
        // unless the token is an automation token, and those are the only kind
        // that bypass 2FA.
        replayPublishLogs()
        process.exit(1)
      }
      await sleep(wait)
      wait = Math.min(wait * 2, 15000)
    }
  }
}

main()
