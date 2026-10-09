import path from "node:path"

import dts from "rollup-plugin-dts"

const layers = "@dagger.io/"

export default {
  input: "./dist/src/index.d.ts",
  output: {
    file: "dist/core.d.ts",
    format: "es",
  },
  plugins: [
    // The facade imports the layers as packages, which resolve through the
    // workspace symlinks into node_modules — and rollup-plugin-dts leaves
    // node_modules external, so without this core.d.ts came out as seven lines
    // of `export * from '@dagger.io/...'` and nothing a module could type-check
    // against. Point the specifiers at the layers' built declarations instead so
    // they inline, which is what a single core.d.ts beside core.js has to be.
    {
      name: "inline-workspace-layers",
      resolveId(id) {
        if (!id.startsWith(layers)) return null
        return path.resolve(`src/${id.slice(layers.length)}/dist/index.d.ts`)
      },
    },
    dts(),
  ],
}
