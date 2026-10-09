/**
 * The `@dagger.io/module` entry point: the decorators a module author writes,
 * and the one lookup a dispatcher needs.
 *
 * Deliberately not the whole layer. `registry` itself stays unexported — it is
 * the mutable singleton the decorators write into, and `getRegisteredClass` is
 * the read side. `introspector/` is absent too: it is generation-time, it pulls
 * the TypeScript compiler, and no published entry point reaches it.
 */
export * from "./decorators.js"
export { getRegisteredClass } from "./registry.js"
