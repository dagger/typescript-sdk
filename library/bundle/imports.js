// src/module/introspector/imports_entrypoint.ts
import * as path3 from "path";

// src/module/introspector/imports.ts
import * as fs2 from "fs";
import * as path2 from "path";
import ts from "typescript";

// src/module/introspector/utils/files.ts
import * as fs from "fs";
import * as path from "path";
var allowedExtensions = [".ts", ".mts"];
async function listFiles(dir = ".") {
  const res = await Promise.all(fs.readdirSync(dir).map(async (file) => {
    const filepath = path.join(dir, file);
    if (filepath.includes("node_modules") || filepath.includes("dist")) {
      return [];
    }
    const stat = fs.statSync(filepath);
    if (stat.isDirectory()) {
      return await listFiles(filepath);
    }
    const ext = path.extname(filepath);
    if (allowedExtensions.find((allowedExt) => allowedExt === ext)) {
      return [path.join(dir, file)];
    }
    return [];
  }));
  return res.reduce((p, c) => [...c, ...p], []);
}

// src/module/introspector/imports.ts
async function deriveResources(workspaceRoot, modulePath, sourceDir) {
  const moduleRoot = path2.resolve(workspaceRoot, modulePath);
  const found = new Map;
  const add = (target, origin) => {
    const pattern = outsidePattern(moduleRoot, target);
    if (pattern !== undefined && !found.has(pattern)) {
      found.set(pattern, origin);
    }
  };
  const queue = await listFiles(path2.join(moduleRoot, sourceDir));
  const seen = new Set(queue);
  const follow = (file) => {
    for (const specifier of importedFiles(file)) {
      if (!specifier.startsWith(".")) {
        continue;
      }
      const target = resolveRelative(file, specifier);
      if (target === undefined) {
        continue;
      }
      add(path2.dirname(target), file);
      if (!seen.has(target)) {
        seen.add(target);
        queue.push(target);
      }
    }
  };
  while (queue.length > 0) {
    follow(queue.shift());
  }
  for (const { target, origin, followable } of configuredTargets(moduleRoot)) {
    add(target, origin);
    if (!followable) {
      continue;
    }
    for (const file of filesUnder(target)) {
      if (!seen.has(file)) {
        seen.add(file);
        queue.push(file);
      }
    }
  }
  while (queue.length > 0) {
    follow(queue.shift());
  }
  return [...found].map(([pattern, origin]) => ({ pattern, origin })).sort((a, b) => a.pattern < b.pattern ? -1 : 1);
}
function filesUnder(target) {
  if (!fs2.existsSync(target)) {
    return [];
  }
  if (fs2.statSync(target).isFile()) {
    return [target];
  }
  const out = [];
  for (const entry of fs2.readdirSync(target)) {
    const child = path2.join(target, entry);
    if (entry === "node_modules") {
      continue;
    }
    out.push(...filesUnder(child));
  }
  return out.filter((f) => /\.(ts|mts|cts|tsx)$/.test(f));
}
function importedFiles(file) {
  let text;
  try {
    text = fs2.readFileSync(file, "utf8");
  } catch {
    return [];
  }
  const info = ts.preProcessFile(text, true, true);
  return [...info.importedFiles, ...info.referencedFiles].map((f) => f.fileName);
}
function resolveRelative(from, specifier) {
  const base = path2.resolve(path2.dirname(from), specifier);
  const exts = [".ts", ".mts", ".cts", ".tsx", ".d.ts"];
  const candidates = [base];
  const swapped = base.replace(/\.(js|mjs|cjs|jsx)$/, "");
  if (swapped !== base) {
    candidates.push(...exts.map((e) => swapped + e));
  }
  candidates.push(...exts.map((e) => base + e));
  candidates.push(...exts.map((e) => path2.join(base, "index" + e)));
  for (const candidate of candidates) {
    if (fs2.existsSync(candidate) && fs2.statSync(candidate).isFile()) {
      return candidate;
    }
  }
  if (fs2.existsSync(base) && fs2.statSync(base).isDirectory()) {
    return path2.join(base, "index.ts");
  }
  return;
}
function outsidePattern(moduleRoot, target) {
  const rel = path2.relative(moduleRoot, target);
  if (rel === "" || !rel.startsWith("..")) {
    return;
  }
  return rel;
}
function configuredTargets(moduleRoot) {
  const out = [];
  const pkgPath = path2.join(moduleRoot, "package.json");
  const pkg = readJSON(pkgPath);
  if (pkg) {
    for (const field of ["dependencies", "devDependencies"]) {
      const deps = pkg[field];
      if (typeof deps !== "object" || deps === null) {
        continue;
      }
      for (const [name, spec] of Object.entries(deps)) {
        if (typeof spec !== "string") {
          continue;
        }
        const local = localDependencyPath(spec);
        if (local !== undefined) {
          out.push({
            target: path2.resolve(moduleRoot, local),
            origin: `${pkgPath} (${field}.${name})`,
            followable: true
          });
        }
      }
    }
  }
  const tsconfigPath = path2.join(moduleRoot, "tsconfig.json");
  const tsconfig = readJSON(tsconfigPath);
  const extendsField = tsconfig?.extends;
  const extendsList = Array.isArray(extendsField) ? extendsField : typeof extendsField === "string" ? [extendsField] : [];
  for (const target of extendsList) {
    if (target.startsWith(".")) {
      out.push({
        target: path2.resolve(moduleRoot, target),
        origin: `${tsconfigPath} (extends)`,
        followable: false
      });
    }
  }
  const baseUrl = tsconfig?.compilerOptions?.baseUrl;
  const pathsBase = typeof baseUrl === "string" ? path2.resolve(moduleRoot, baseUrl) : moduleRoot;
  const paths = tsconfig?.compilerOptions?.paths;
  if (typeof paths === "object" && paths !== null) {
    for (const [alias, targets] of Object.entries(paths)) {
      if (!Array.isArray(targets)) {
        continue;
      }
      for (const target of targets) {
        if (typeof target !== "string") {
          continue;
        }
        out.push({
          target: path2.resolve(pathsBase, withoutGlob(target)),
          origin: `${tsconfigPath} (paths.${alias})`,
          followable: true
        });
      }
    }
  }
  const denoPath = path2.join(moduleRoot, "deno.json");
  const imports = readJSON(denoPath)?.imports;
  if (typeof imports === "object" && imports !== null) {
    for (const [alias, target] of Object.entries(imports)) {
      if (typeof target === "string" && target.startsWith(".")) {
        out.push({
          target: path2.resolve(moduleRoot, withoutGlob(target)),
          origin: `${denoPath} (imports.${alias})`,
          followable: true
        });
      }
    }
  }
  return out;
}
function localDependencyPath(spec) {
  if (spec.startsWith("file:")) {
    return spec.slice("file:".length);
  }
  if (spec.startsWith("link:")) {
    return spec.slice("link:".length);
  }
  return;
}
function withoutGlob(target) {
  const segments = target.split("/");
  const glob = segments.findIndex((s) => /[*?[]/.test(s));
  return glob === -1 ? target : segments.slice(0, glob).join("/");
}
function readJSON(file) {
  try {
    return JSON.parse(fs2.readFileSync(file, "utf8"));
  } catch {
    return;
  }
}

// src/module/introspector/imports_entrypoint.ts
async function main() {
  const args = process.argv.slice(2);
  if (args.length < 3) {
    console.log("usage: imports <workspaceRoot> <modulePath> <sourceDir>");
    process.exit(1);
  }
  const [workspaceRoot, modulePath, sourceDir] = args;
  const base = path3.posix.normalize(modulePath) === "." ? "" : modulePath;
  for (const { pattern, origin } of await deriveResources(workspaceRoot, modulePath, sourceDir)) {
    const resolved = path3.posix.join(base, pattern);
    if (resolved === ".." || resolved.startsWith("../")) {
      console.error(`${relative3(workspaceRoot, origin)} imports ${pattern}, which is outside the workspace root`);
      process.exit(1);
    }
    console.error(`derived ${resolved} from ${relative3(workspaceRoot, origin)}`);
    console.log(resolved);
  }
}
function relative3(workspaceRoot, origin) {
  const rel = path3.relative(workspaceRoot, origin);
  return rel.startsWith("..") ? origin : rel;
}
main();
