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
async function deriveResources(moduleRoot, sourceDir) {
  const found = new Map;
  const add = (dir, origin) => {
    const pattern = outsidePattern(moduleRoot, dir);
    if (pattern !== undefined && !found.has(pattern)) {
      found.set(pattern, origin);
    }
  };
  for (const file of await listFiles(path2.join(moduleRoot, sourceDir))) {
    for (const specifier of importedFiles(file)) {
      if (specifier.startsWith(".")) {
        const resolved = path2.resolve(path2.dirname(file), specifier);
        add(path2.dirname(resolved), file);
      }
    }
  }
  for (const { target, origin } of configuredTargets(moduleRoot)) {
    add(target, origin);
  }
  return [...found].map(([pattern, origin]) => ({ pattern, origin })).sort((a, b) => a.pattern < b.pattern ? -1 : 1);
}
function importedFiles(file) {
  const text = fs2.readFileSync(file, "utf8");
  return ts.preProcessFile(text, true, true).importedFiles.map((f) => f.fileName);
}
function outsidePattern(moduleRoot, dir) {
  const rel = path2.relative(path2.resolve(moduleRoot), dir);
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
            origin: `${pkgPath} (${field}.${name})`
          });
        }
      }
    }
  }
  const tsconfigPath = path2.join(moduleRoot, "tsconfig.json");
  const tsconfig = readJSON(tsconfigPath);
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
          target: path2.resolve(moduleRoot, withoutGlob(target)),
          origin: `${tsconfigPath} (paths.${alias})`
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
    console.log("usage: imports <moduleRoot> <sourceDir> <modulePath>");
    process.exit(1);
  }
  const [moduleRoot, sourceDir, modulePath] = args;
  const base = path3.posix.normalize(modulePath) === "." ? "" : modulePath;
  for (const { pattern, origin } of await deriveResources(moduleRoot, sourceDir)) {
    const resolved = path3.posix.join(base, pattern);
    if (resolved === ".." || resolved.startsWith("../")) {
      console.error(`${origin} imports ${pattern}, which is outside the workspace root`);
      process.exit(1);
    }
    console.log(resolved);
  }
}
main();
