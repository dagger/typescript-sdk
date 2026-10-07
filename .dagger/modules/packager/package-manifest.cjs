// Write one layer's published manifest, deriving its dependencies from what the
// built code imports.
//
// Derived rather than written down, because a hand-kept list per package is four
// lists that drift — and the drift is invisible until a consumer installs the
// package and an import fails. Versions come from the library's own manifest, so
// there is still one place where a range is chosen.
//
// Usage: node package-manifest.cjs <layer dist> <library package.json> <name> <version> <out>
const fs = require("fs")

const { externalImports } = require("./imports.cjs")

const [layerDist, libraryManifestPath, name, version, out] = process.argv.slice(2)

const library = JSON.parse(fs.readFileSync(libraryManifestPath, "utf8"))
const imported = externalImports(layerDist)

const dependencies = {}
const undeclared = []
for (const [pkg, file] of [...imported].sort()) {
  if (pkg.startsWith("@dagger.io/")) {
    // The split's own packages move together, so a sibling is pinned exactly:
    // session and core are only ever consumed at the version they shipped with.
    dependencies[pkg] = version
    continue
  }
  const range = library.dependencies?.[pkg]
  if (!range) {
    undeclared.push(`  ${pkg}  (${file})`)
    continue
  }
  dependencies[pkg] = range
}

if (undeclared.length > 0) {
  console.error(
    `${name} imports packages the library's manifest does not declare, so there ` +
      `is no range to publish them at:\n${undeclared.join("\n")}`,
  )
  process.exit(1)
}

fs.writeFileSync(
  out,
  JSON.stringify(
    {
      name,
      version,
      author: library.author,
      license: library.license,
      type: "module",
      main: "./dist/index.js",
      types: "./dist/index.d.ts",
      exports: { ".": "./dist/index.js" },
      files: ["dist/"],
      engines: library.engines,
      dependencies,
    },
    null,
    2,
  ) + "\n",
)

console.log(
  `${name}@${version}: ${Object.keys(dependencies).length} dependencies ` +
    `(${Object.keys(dependencies).join(", ") || "none"})`,
)
