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

// core is pinned to a *bounded* range rather than that exact version, so a
// patch under the same engine release is picked up without regenerating every
// scope. core's version is the engine's, so when it has to be republished
// without the engine moving it becomes e.g. 1.0.0-beta.25.2 — and nothing can
// resolve that while the pin is exact.
//
// Bounded and not `^`: a caret on a prerelease also admits the *next* engine
// beta (semver says ^1.0.0-beta.25 satisfies 1.0.0-beta.26), which would let
// bindings drift from the engine they were generated for. The upper bound is
// the next beta, so only patches of this one get in.
const nextEngineVersion = coreVersion.replace(
  /(\d+)$/,
  (n) => String(Number(n) + 1),
)
if (nextEngineVersion === coreVersion) {
  console.error(`cannot derive an upper bound from ${coreVersion}`)
  process.exit(1)
}
const coreRange = `>=${coreVersion} <${nextEngineVersion}`
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
    "@dagger.io/core": coreRange,
    // Declared, not left to hoisting: index.ts re-exports from session
    // directly, and an undeclared transitive import is what Deno refuses.
    "@dagger.io/session": layerVersion("session"),
    // `connection` lives here now rather than in the facade's own source.
    "@dagger.io/connect": layerVersion("connect"),
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
