import { dag, check, func, object } from "@dagger.io/dagger"

/**
 * A pre-1.0 module: its only config is a dagger.json, so the engine's builtin
 * TypeScript runtime loads it and does its codegen at call time.
 */
@object()
export class RuntimeLegacyApp {
  /** Reaches the dependency the dagger.json declares — the edge a lossy migration dropped. */
  @func()
  @check()
  async dependencyCheck(): Promise<void> {
    const got = await dag.runtimeLegacyDep().dep()
    if (got !== "dep") {
      throw new Error(`the declared dependency returned ${got}`)
    }
  }
}
