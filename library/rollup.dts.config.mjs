import dts from "rollup-plugin-dts"

export default {
  input: "./dist/src/index.d.ts", // or wherever your entrypoint is
  output: {
    file: "dist/core.d.ts",
    format: "es",
  },
  // Kept external, against respectExternal below. node builtins are honestly
  // @types/node's job, and inlining reflect-metadata would bake its `declare
  // global { namespace Reflect }` block into every generated module — globals
  // this file has no business declaring, which collide with the real package
  // and are the only CRLF in the output (git normalizes them on commit, so the
  // committed bundle would never match what the packager builds).
  external: (id) => id === "reflect-metadata" || id.startsWith("node:"),
  // Everything else is inlined rather than imported. core.js already bundles
  // graphql-request, graphql and @opentelemetry/api, so leaving their *types*
  // behind as imports is the only thing that still named them: `tsc` on a
  // generated module answered with six TS2307s unless the user installed the
  // packages the bundle exists to hide, and Deno — which resolves core.d.ts
  // through its own import map — either refused them outright (2.1) or quietly
  // fetched three npm packages to type-check them (2.5). Costs ~76 KB against
  // a 4 MB bundle.
  plugins: [dts({ respectExternal: true })],
}
