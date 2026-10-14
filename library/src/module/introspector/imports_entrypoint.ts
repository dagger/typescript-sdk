import * as path from "path"

import { deriveResources } from "./imports.js"

/**
 * Print the paths a module needs from outside its own directory, derived from
 * what its source imports.
 *
 * One workspace-relative pattern per line, which is the form the SDK's own
 * reads take; codegen rebases them to module-relative for the `resources`
 * member the engine resolves. Nothing is printed for a module that imports
 * nothing from outside, which is the usual case.
 *
 * `modulePath` is the module directory relative to the workspace root. A
 * pattern that climbs past that root is refused rather than clamped: the SDK
 * cannot read above it to scan the module, so the build could not work either
 * way, and the error names the import or config line behind it.
 */
async function main() {
  const args = process.argv.slice(2)
  if (args.length < 3) {
    console.log("usage: imports <moduleRoot> <sourceDir> <modulePath>")
    process.exit(1)
  }

  const [moduleRoot, sourceDir, modulePath] = args
  const base = path.posix.normalize(modulePath) === "." ? "" : modulePath

  for (const { pattern, origin } of await deriveResources(
    moduleRoot,
    sourceDir,
  )) {
    const resolved = path.posix.join(base, pattern)
    if (resolved === ".." || resolved.startsWith("../")) {
      console.error(
        `${origin} imports ${pattern}, which is outside the workspace root`,
      )
      process.exit(1)
    }

    console.log(resolved)
  }
}

main()
