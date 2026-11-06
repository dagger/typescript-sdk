// Assemble the facade package's manifest: @dagger.io/dagger as a thin package
// over the published layers, for a module that resolves them from a registry
// instead of carrying the bundle.
//
// The versions are not invented here. core's is the engine release the library
// announces, because core is the bindings for one engine and ships from
// dagger/dagger at that version. The other three are whatever the committed
// manifests say, so a generated module pins exactly what this bundle was built
// against rather than a range that could drift.
//
// Usage: node facade-manifest.cjs <engine-version-file> <out>
const fs = require("fs")

const [engineVersionPath, out] = process.argv.slice(2)
if (!engineVersionPath || !out) {
  console.error("usage: facade-manifest.cjs <engine-version-file> <out>")
  process.exit(1)
}

// `v1.0.0-beta.25` in the file, `1.0.0-beta.25` on npm.
const coreVersion = fs.readFileSync(engineVersionPath, "utf8").trim().replace(/^v/, "")
const layerVersion = (layer) =>
  JSON.parse(fs.readFileSync(`src/${layer}/package.json`, "utf8")).version

const manifest = {
  name: "@dagger.io/dagger",
  version: coreVersion,
  type: "module",
  main: "./index.ts",
  types: "./index.ts",
  exports: {
    ".": "./index.ts",
    "./telemetry": "./telemetry.ts",
  },
  dependencies: {
    "@dagger.io/core": coreVersion,
    "@dagger.io/module": layerVersion("module"),
    "@dagger.io/telemetry": layerVersion("telemetry"),
    // The facade re-exports GraphQLClient, which is the only thing it needs
    // that no layer provides.
    "graphql-request": JSON.parse(fs.readFileSync("package.json", "utf8"))
      .dependencies["graphql-request"],
  },
}

fs.writeFileSync(out, JSON.stringify(manifest, null, 2) + "\n")
console.log(`${manifest.name}@${manifest.version} over core ${coreVersion}`)
