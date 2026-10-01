// Compare what a built package imports against what its manifest declares, and
// fail on either kind of disagreement.
//
// Run over packager:publishable's output, so the input is compiled JavaScript:
// `import type` is already erased, which is what makes the comparison meaningful
// — a type-only import is not a runtime dependency.
//
// Usage: node audit-imports.cjs <package root>
const fs = require("fs")
const path = require("path")

const builtins = new Set(require("module").builtinModules)

// Declared to satisfy another dependency's peer range rather than because this
// code imports it. graphql-request peers on graphql "14 - 16"; the library's own
// use of graphql is two `import type`s, so it is invisible here by design.
const peerOnly = new Set(["graphql"])

const root = process.argv[2]
const manifest = JSON.parse(
  fs.readFileSync(path.join(root, "package.json"), "utf8"),
)
const declared = new Set(Object.keys(manifest.dependencies ?? {}))

// Static imports are matched anchored at the start of a line, because tsc emits
// one per line and client.gen.js is 20k lines of doc comments that otherwise
// match on prose like `... from "alpine"`. Dynamic import() and require() can sit
// anywhere, so those are matched loosely and filtered by shape below.
const statements = [
  /^\s*(?:import|export)\b[^;\n]*?\bfrom\s*["']([^"']+)["']/gm,
  /^\s*import\s*["']([^"']+)["']/gm,
  /(?:require|import)\(\s*["']([^"']+)["']\s*\)/g,
]

// A specifier, not a sentence: no whitespace, and a name before any subpath.
const specifierShape = /^(?:@[^/\s]+\/)?[^/\s]+(?:\/[^\s]*)?$/

function sources(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) return sources(full)
    return entry.isFile() && full.endsWith(".js") ? [full] : []
  })
}

function packageOf(specifier) {
  const parts = specifier.split("/")
  return specifier.startsWith("@") ? parts.slice(0, 2).join("/") : parts[0]
}

const imported = new Map()
for (const file of sources(path.join(root, "dist"))) {
  const source = fs.readFileSync(file, "utf8")
  for (const pattern of statements) {
    for (const [, spec] of source.matchAll(pattern)) {
      if (spec.startsWith(".") || spec.startsWith("node:")) continue
      if (!specifierShape.test(spec)) continue
      const name = packageOf(spec)
      if (builtins.has(name)) continue
      if (!imported.has(name)) imported.set(name, path.relative(root, file))
    }
  }
}

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
