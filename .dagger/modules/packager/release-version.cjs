// Stamp a release version onto a committed package manifest.
//
// The manifests under library/src/*/ are committed with version 0.0.0 and pin
// their siblings at 0.0.0, the same placeholder library/package.json has always
// carried — nothing in this tree is a release until something cuts one. This is
// the one step that turns a placeholder into a version, and it moves the sibling
// pins with it: session and the packages over it only ever ship together.
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
