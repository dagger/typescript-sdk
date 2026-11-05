import { object, func, check } from "@dagger.io/dagger"

@object()
export class RuntimeDeclaredBun {
  /**
   * Passes only when the call ran on bun, which this module asks for through
   * package.json's `dagger.runtime` alone — there is no bun lockfile here to
   * detect it from.
   */
  @func()
  @check()
  bunRuntimeCheck(): void {
    const versions = process.versions as Record<string, string | undefined>
    if (!versions.bun) {
      throw new Error(
        `expected bun from package.json dagger.runtime, ran on node ${process.versions.node}`,
      )
    }
  }
}
