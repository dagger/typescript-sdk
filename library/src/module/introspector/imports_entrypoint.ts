import * as path from "path"

import { deriveResources } from "./imports.js"

/**
 * Print the paths a module needs from outside its own directory, derived from
 * what its code imports.
 *
 * One workspace-relative pattern per line on stdout, which is the form the
 * SDK's own reads take; codegen rebases them for the `sources` member the
 * engine calls. Nothing is printed for a module that imports nothing from
 * outside, which is the usual case.
 *
 * Each one is also written to stderr with the import behind it, so
 * `dagger generate` shows in its trace what the module's code asked for. The
 * set is implicit by design — the code is the single source of truth — but
 * implicit is not the same as invisible, and a surprise entry is far easier to
 * place when the line names the file that caused it.
 *
 * `modulePath` is the module directory relative to the workspace root. A
 * pattern that climbs past that root is refused rather than clamped: the SDK
 * cannot read above it to scan the module, so no resource would make the build
 * work.
 */
async function main() {
  const args = process.argv.slice(2)
  if (args.length < 3) {
    console.log("usage: imports <workspaceRoot> <modulePath> <sourceDir>")
    process.exit(1)
  }

  const [workspaceRoot, modulePath, sourceDir] = args
  const base = path.posix.normalize(modulePath) === "." ? "" : modulePath

  for (const { pattern, origin } of await deriveResources(
    workspaceRoot,
    modulePath,
    sourceDir,
  )) {
    const resolved = path.posix.join(base, pattern)
    if (resolved === ".." || resolved.startsWith("../")) {
      console.error(
        `${relative(workspaceRoot, origin)} imports ${pattern}, which is outside the workspace root`,
      )
      process.exit(1)
    }

    console.error(
      `derived ${resolved} from ${relative(workspaceRoot, origin)}`,
    )
    console.log(resolved)
  }
}

/**
 * An origin as the user would recognise it: workspace-relative, since the
 * absolute path it carries is a scratch directory inside a container.
 */
function relative(workspaceRoot: string, origin: string): string {
  const rel = path.relative(workspaceRoot, origin)

  return rel.startsWith("..") ? origin : rel
}

main()
