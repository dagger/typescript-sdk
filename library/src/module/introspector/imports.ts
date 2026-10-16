import * as fs from "fs"
import * as path from "path"
import ts from "typescript"

import { listFiles } from "./utils/files.js"

/**
 * One path a module needs from outside its own directory, and why.
 *
 * `pattern` is relative to the module directory. `origin` is what asked for it,
 * so the line `dagger generate` prints names the import a user would have to
 * change rather than a path they never wrote.
 */
export type DerivedResource = {
  pattern: string
  origin: string
}

/**
 * Paths outside the module directory that the module's code needs, derived from
 * what it imports rather than from a list someone maintains.
 *
 * Transitively: an import that leaves the module directory is followed, and
 * whatever *that* file imports is followed too, until nothing new turns up.
 * Stopping at the first hop would only ever work by accident — a sibling
 * directory importing from a third one would scan here and fail to compile in
 * the container.
 *
 * Three ways a specifier reaches outside, all of them the user's own code or
 * config:
 *
 * - A relative specifier that resolves outside the module directory.
 * - A `file:` or `link:` dependency in package.json, which is how a bare
 *   specifier legitimately points outside — the package manager resolves it
 *   through a symlink static analysis cannot follow. A `workspace:` dependency
 *   names no path of its own, so it is not resolvable here.
 * - A tsconfig `paths` target (resolved against `baseUrl` when set) or an
 *   `extends`, or a deno.json `imports` entry — a Deno module has no
 *   package.json and no tsconfig, so its import map is the only alias it has.
 *
 * A bare specifier with none of those is left alone: it resolves inside
 * `node_modules`, and in a monorepo that directory sits at the repository root,
 * outside the module and gitignored — naming it would name a path the engine
 * drops from the context anyway.
 */
export async function deriveResources(
  workspaceRoot: string,
  modulePath: string,
  sourceDir: string,
): Promise<DerivedResource[]> {
  const moduleRoot = path.resolve(workspaceRoot, modulePath)
  const found = new Map<string, string>()

  const add = (target: string, origin: string) => {
    const pattern = outsidePattern(moduleRoot, target)
    if (pattern !== undefined && !found.has(pattern)) {
      found.set(pattern, origin)
    }
  }

  // Breadth-first, so a path is attributed to the first import that reached it
  // — the one nearest the module's own source, which is the one a user can act
  // on.
  const queue = await listFiles(path.join(moduleRoot, sourceDir))
  const seen = new Set(queue)

  const follow = (file: string) => {
    for (const specifier of importedFiles(file)) {
      if (!specifier.startsWith(".")) {
        continue
      }

      const target = resolveRelative(file, specifier)
      if (target === undefined) {
        continue
      }

      // The directory, not the file: a specifier names `./greet.js` where the
      // file on disk is `greet.ts`, so the resolved path often does not exist
      // as written, and the directory covers whatever that file reads from
      // beside it.
      add(path.dirname(target), file)

      if (!seen.has(target)) {
        seen.add(target)
        queue.push(target)
      }
    }
  }

  while (queue.length > 0) {
    follow(queue.shift() as string)
  }

  // A config target is taken at the granularity the config named it: a file
  // stays a file, since its path is exact and its directory can be the whole
  // repository root.
  for (const { target, origin, followable } of configuredTargets(moduleRoot)) {
    add(target, origin)

    if (!followable) {
      continue
    }

    // No guard on `target` itself: when it names a file, filesUnder returns
    // that very path, and marking it seen first would drop the one file the
    // alias points at — which is how a Deno import map's target went
    // unfollowed.
    for (const file of filesUnder(target)) {
      if (!seen.has(file)) {
        seen.add(file)
        queue.push(file)
      }
    }
  }

  // Whatever the config pulled in can import outward in turn.
  while (queue.length > 0) {
    follow(queue.shift() as string)
  }

  return [...found]
    .map(([pattern, origin]) => ({ pattern, origin }))
    .sort((a, b) => (a.pattern < b.pattern ? -1 : 1))
}

/**
 * The TypeScript files at `target`, which may name one file or a directory.
 */
function filesUnder(target: string): string[] {
  if (!fs.existsSync(target)) {
    return []
  }

  if (fs.statSync(target).isFile()) {
    return [target]
  }

  const out: string[] = []
  for (const entry of fs.readdirSync(target)) {
    const child = path.join(target, entry)
    if (entry === "node_modules") {
      continue
    }
    out.push(...filesUnder(child))
  }

  return out.filter((f) => /\.(ts|mts|cts|tsx)$/.test(f))
}

/**
 * Every module specifier one file imports, including type-only imports,
 * `export * from`, dynamic `import()` and `/// <reference path=...>`.
 *
 * `preProcessFile` is a lexical scan: it needs no program and no type checker,
 * only the one file's text. Nothing here has to resolve before it reads, which
 * is what lets the first pass run against the module alone.
 */
function importedFiles(file: string): string[] {
  let text: string
  try {
    text = fs.readFileSync(file, "utf8")
  } catch {
    return []
  }

  const info = ts.preProcessFile(text, true, true)

  return [...info.importedFiles, ...info.referencedFiles].map((f) => f.fileName)
}

/**
 * The file a relative specifier resolves to, or undefined when nothing on disk
 * matches it.
 *
 * TypeScript spells an import of `greet.ts` as `./greet.js`, so the written
 * path usually does not exist: the extension has to be swapped back, and a
 * directory import has to find its index file.
 */
function resolveRelative(from: string, specifier: string): string | undefined {
  const base = path.resolve(path.dirname(from), specifier)
  const exts = [".ts", ".mts", ".cts", ".tsx", ".d.ts"]

  const candidates = [base]
  const swapped = base.replace(/\.(js|mjs|cjs|jsx)$/, "")
  if (swapped !== base) {
    candidates.push(...exts.map((e) => swapped + e))
  }
  candidates.push(...exts.map((e) => base + e))
  candidates.push(...exts.map((e) => path.join(base, "index" + e)))

  for (const candidate of candidates) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
      return candidate
    }
  }

  // Nothing resolved. A directory still counts: the import is broken either
  // way, but if the directory is there it is what the module meant to reach,
  // and naming it beats dropping it silently.
  if (fs.existsSync(base) && fs.statSync(base).isDirectory()) {
    return path.join(base, "index.ts")
  }

  return undefined
}

/**
 * `target` as a module-relative pattern, or undefined when it is inside the
 * module directory and so already part of its source.
 */
function outsidePattern(moduleRoot: string, target: string): string | undefined {
  const rel = path.relative(moduleRoot, target)
  if (rel === "" || !rel.startsWith("..")) {
    return undefined
  }

  return rel
}

/**
 * Targets the module's own config points outside its directory.
 *
 * `followable` marks the ones whose contents can import further: a mapped
 * module can, a tsconfig cannot.
 */
function configuredTargets(
  moduleRoot: string,
): { target: string; origin: string; followable: boolean }[] {
  const out: { target: string; origin: string; followable: boolean }[] = []

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
            followable: true,
          })
        }
      }
    }
  }

  const tsconfigPath = path.join(moduleRoot, "tsconfig.json")
  const tsconfig = readJSON(tsconfigPath)

  // The file itself, not its directory: an extends target is an exact path, and
  // its directory is the module's parent — including that would pull in the
  // repository. Missing it is no quiet loss either: tsx reads the chain at call
  // time and dies with `File '../tsconfig.base.json' not found.`
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
        followable: false,
      })
    }
  }

  // `paths` resolve against `baseUrl` when it is set, and against the
  // tsconfig's own directory when it is not.
  const baseUrl = tsconfig?.compilerOptions?.baseUrl
  const pathsBase =
    typeof baseUrl === "string" ? path.resolve(moduleRoot, baseUrl) : moduleRoot

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
          target: path.resolve(pathsBase, withoutGlob(target)),
          origin: `${tsconfigPath} (paths.${alias})`,
          followable: true,
        })
      }
    }
  }

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
          followable: true,
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
 * what a nested specifier such as `@alias/deep/thing` needs.
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
