// Which packages a built tree actually imports.
//
// Shared by audit-imports.cjs, which compares the answer against a hand-written
// manifest, and package-manifest.cjs, which turns it into one. They have to
// agree on what counts as a dependency or the generated manifests and the audit
// would be measuring different things.
//
// Run over compiled JavaScript, never TypeScript: `import type` is already
// erased by then, and a type-only import is not a runtime dependency.
const fs = require("fs")
const path = require("path")

const builtins = new Set(require("module").builtinModules)

// Static imports are matched anchored at the start of a line, because tsc emits
// one per line and client.gen.js is 20k lines of doc comments that otherwise
// match on prose like `... from "alpine"`. Dynamic import() and require() can sit
// anywhere, so those are matched loosely and filtered by shape below.
const patterns = [
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

/**
 * Every package imported under `dir`, mapped to the first file that imports it
 * — relative to `relativeTo` so the name is readable in an error.
 */
function externalImports(dir, relativeTo = dir) {
  const found = new Map()
  for (const file of sources(dir)) {
    const source = fs.readFileSync(file, "utf8")
    for (const pattern of patterns) {
      for (const [, spec] of source.matchAll(pattern)) {
        if (spec.startsWith(".") || spec.startsWith("node:")) continue
        if (!specifierShape.test(spec)) continue
        const name = packageOf(spec)
        if (builtins.has(name)) continue
        if (!found.has(name)) found.set(name, path.relative(relativeTo, file))
      }
    }
  }
  return found
}

module.exports = { externalImports }
