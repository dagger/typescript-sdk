// Compare what a built package imports against what its manifest declares, and
// fail on either kind of disagreement.
//
// Run over a built package — dist/ beside its package.json — so the input is JavaScript:
// `import type` is already erased, which is what makes the comparison meaningful
// — a type-only import is not a runtime dependency.
//
// Usage: node audit-imports.cjs <package root>
const fs = require("fs")
const path = require("path")

const { externalImports } = require("./imports.cjs")

// Declared to satisfy another dependency's peer range rather than because this
// code imports it. graphql-request peers on graphql "14 - 16"; the library's own
// use of graphql is two `import type`s, so it is invisible here by design.
const peerOnly = new Set(["graphql"])

const root = process.argv[2]
const manifest = JSON.parse(
  fs.readFileSync(path.join(root, "package.json"), "utf8"),
)
const declared = new Set(Object.keys(manifest.dependencies ?? {}))

const imported = externalImports(path.join(root, "dist"), root)

const missing = [...imported].filter(([name]) => !declared.has(name))
const unused = [...declared].filter(
  (name) => !imported.has(name) && !peerOnly.has(name),
)

const problems = []
if (missing.length > 0) {
  problems.push(
    "imported but not declared — resolves today only because something else " +
      "happens to pull it in:\n" +
      missing.map(([name, file]) => `  ${name}  (${file})`).join("\n"),
  )
}
if (unused.length > 0) {
  problems.push(
    "declared but never imported — every consumer installs these for nothing:\n" +
      unused.sort().map((name) => `  ${name}`).join("\n"),
  )
}

if (problems.length > 0) {
  console.error(problems.join("\n\n"))
  process.exit(1)
}

console.log(
  `ok: ${imported.size} packages imported, ${declared.size} declared` +
    (peerOnly.size > 0 ? `, ${peerOnly.size} declared for a peer range` : ""),
)
