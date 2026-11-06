// Stamp a release version onto a committed package manifest.
//
// The manifests under library/src/*/ carry the version last released from here,
// and pin their siblings at it. That is a record, not a promise: what actually
// ships is whatever a release passes, so this step overwrites both — and it moves
// the sibling pins with the version, because session and the packages over it
// only ever ship together.
//
// core is the exception: it is published from dagger/dagger at the engine's
// version, so only its pin on session is meaningful here.
//
// Usage: node release-version.cjs <package.json> <out>   (version from $PUBLISH_VERSION)
const fs = require("fs")

const [manifestPath, out] = process.argv.slice(2)
const version = process.env.PUBLISH_VERSION
if (!version) {
  console.error("PUBLISH_VERSION is not set")
  process.exit(1)
}

const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"))
manifest.version = version
for (const name of Object.keys(manifest.dependencies ?? {})) {
  if (name.startsWith("@dagger.io/")) {
    manifest.dependencies[name] = version
  }
}

fs.writeFileSync(out, JSON.stringify(manifest, null, 2) + "\n")
console.log(`${manifest.name}@${version}`)
