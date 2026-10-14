import * as fs from "fs"
import * as path from "path"
import ts from "typescript"

import { listFiles } from "./utils/files.js"

/**
 * One path a module needs from outside its own directory, and why.
 *
 * `pattern` is relative to the module directory, the form the engine resolves
 * `resources` in. `origin` is what asked for it, so a refusal can name the line
 * a user would have to change rather than a path they never wrote.
 */
export type DerivedResource = {
  pattern: string
  origin: string
}

/**
 * Paths outside `moduleRoot` that the module's own source needs, derived from
 * what it imports rather than from a list someone maintains.
 *
 * Three sources, because a specifier reaches outside the module directory in
 * three different ways:
 *
 * - A relative specifier that climbs out of it (`../../shared/greet.js`).
 * - A `file:` or `link:` dependency in package.json, which is how a bare
 *   specifier legitimately points outside — the package manager resolves it
 *   through a symlink that static analysis cannot see. A `workspace:` dependency
 *   names no path of its own, so it is not resolvable here.
 * - A tsconfig `paths` target outside the module directory, or an `extends`
 *   pointing at one.
 * - A deno.json `imports` entry naming a path outside it, which is a Deno
 *   module's only alias mechanism.
 *
 * A bare specifier with no such declaration is left alone: it resolves inside
 * `node_modules`, and in a monorepo that directory sits at the repository root,
 * outside the module and gitignored — chasing it there would name a path the
 * engine drops from the context anyway.
 */
export async function deriveResources(
  moduleRoot: string,
  sourceDir: string,
): Promise<DerivedResource[]> {
  const found = new Map<string, string>()

  // Granularity is the caller's: an import contributes the directory holding
  // its target, because a specifier names `./greet.js` where the file on disk is
  // `greet.ts` — the directory covers both spellings, and whatever that file
  // reads from beside it. A config target that names an exact file contributes
  // the file, since its directory can be the whole repository root.
  const add = (target: string, origin: string) => {
    const pattern = outsidePattern(moduleRoot, target)
    if (pattern !== undefined && !found.has(pattern)) {
      found.set(pattern, origin)
    }
  }

  for (const file of await listFiles(path.join(moduleRoot, sourceDir))) {
    for (const specifier of importedFiles(file)) {
      if (specifier.startsWith(".")) {
        const resolved = path.resolve(path.dirname(file), specifier)
        add(path.dirname(resolved), file)
      }
    }
  }

  // A config target already names a directory.
  for (const { target, origin } of configuredTargets(moduleRoot)) {
    add(target, origin)
  }

  return [...found]
    .map(([pattern, origin]) => ({ pattern, origin }))
    .sort((a, b) => (a.pattern < b.pattern ? -1 : 1))
}

/**
 * Every module specifier one file imports, including type-only imports,
 * `export * from`, and dynamic `import()`.
 *
 * `preProcessFile` is a lexical scan: it needs no program, no type checker and
 * — the point here — none of the files being imported. That is what breaks the
 * circularity, since the files outside the module are exactly what this is
 * trying to discover.
 */
function importedFiles(file: string): string[] {
  const text = fs.readFileSync(file, "utf8")
  const info = ts.preProcessFile(text, true, true)

  // referencedFiles too: a `/// <reference path="../x.d.ts" />` is a build-time
  // path like any import, and the same scan already hands it back.
  return [...info.importedFiles, ...info.referencedFiles].map((f) => f.fileName)
}

/**
 * `target` as a module-relative pattern, or undefined when it is inside the
 * module directory and so already part of its source.
 */
function outsidePattern(moduleRoot: string, target: string): string | undefined {
  const rel = path.relative(path.resolve(moduleRoot), target)
  if (rel === "" || !rel.startsWith("..")) {
    return undefined
  }

  return rel
}

/**
 * Targets the module's own config points outside its directory: `file:` and
 * `link:` dependencies, and tsconfig `paths`.
 *
 * These are declarations the user already maintains for the package manager and
 * the compiler, so reading them asks for nothing new — unlike a list written
 * only for Dagger.
 */
function configuredTargets(
  moduleRoot: string,
): { target: string; origin: string }[] {
  const out: { target: string; origin: string }[] = []

  const pkgPath = path.join(moduleRoot, "package.json")
  const pkg = readJSON(pkgPath)
  if (pkg) {
    for (const field of ["dependencies", "devDependencies"]) {
      const deps = pkg[field]
      if (typeof deps !== "object" || deps === null) {
        continue
      }

      for (const [name, spec] of Object.entries(deps)) {
        if (typeof spec !== "string") {
          continue
        }

        const local = localDependencyPath(spec)
        if (local !== undefined) {
          out.push({
            target: path.resolve(moduleRoot, local),
            origin: `${pkgPath} (${field}.${name})`,
          })
        }
      }
    }
  }

  const tsconfigPath = path.join(moduleRoot, "tsconfig.json")
  const tsconfig = readJSON(tsconfigPath)

  // The file itself, not its directory: `../tsconfig.base.json` sits in the
  // module's parent, and including that whole directory would pull in the
  // repository. An extends target is an exact path, so there is nothing to
  // widen for — unlike an import, whose extension gets rewritten.
  //
  // Missing it is not a quiet loss: tsx reads the chain at call time and dies
  // with `File '../tsconfig.base.json' not found.`
  const extendsField = tsconfig?.extends
  const extendsList = Array.isArray(extendsField)
    ? extendsField
    : typeof extendsField === "string"
      ? [extendsField]
      : []
  for (const target of extendsList) {
    if (target.startsWith(".")) {
      out.push({
        target: path.resolve(moduleRoot, target),
        origin: `${tsconfigPath} (extends)`,
      })
    }
  }

  // Deno's import map, which is a module's only alias mechanism — it has no
  // package.json and no tsconfig. An entry is a path like a tsconfig `paths`
  // target, and the same glob truncation applies.
  const denoPath = path.join(moduleRoot, "deno.json")
  const imports = readJSON(denoPath)?.imports
  if (typeof imports === "object" && imports !== null) {
    for (const [alias, target] of Object.entries(imports)) {
      // A bare target is a registry specifier (`npm:`, `jsr:`, `https:`), not a
      // path. Only a relative one can reach outside the module.
      if (typeof target === "string" && target.startsWith(".")) {
        out.push({
          target: path.resolve(moduleRoot, withoutGlob(target)),
          origin: `${denoPath} (imports.${alias})`,
        })
      }
    }
  }

  const paths = tsconfig?.compilerOptions?.paths
  if (typeof paths === "object" && paths !== null) {
    for (const [alias, targets] of Object.entries(paths)) {
      if (!Array.isArray(targets)) {
        continue
      }

      for (const target of targets) {
        if (typeof target !== "string") {
          continue
        }

        out.push({
          target: path.resolve(moduleRoot, withoutGlob(target)),
          origin: `${tsconfigPath} (paths.${alias})`,
        })
      }
    }
  }

  return out
}

/**
 * The path a dependency specifier points at, for the two protocols that name
 * one, or undefined for anything else — a registry version, or a `workspace:`
 * dependency, which names a member by name and so cannot be resolved here.
 */
function localDependencyPath(spec: string): string | undefined {
  if (spec.startsWith("file:")) {
    return spec.slice("file:".length)
  }

  if (spec.startsWith("link:")) {
    return spec.slice("link:".length)
  }

  return undefined
}

/**
 * `target` truncated at its first glob segment, so an alias like
 * `["../shared/*"]` names the directory rather than one level of its contents —
 * the same granularity a relative import gets, and what a nested specifier such
 * as `@alias/deep/thing` needs.
 */
function withoutGlob(target: string): string {
  const segments = target.split("/")
  const glob = segments.findIndex((s) => /[*?[]/.test(s))

  return glob === -1 ? target : segments.slice(0, glob).join("/")
}

function readJSON(file: string): Record<string, any> | undefined {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"))
  } catch {
    return undefined
  }
}
