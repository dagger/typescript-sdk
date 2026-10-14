import { dag, object, func, check } from "@dagger.io/dagger"

// Calls reach the dependency's interface-returning functions rather than its
// own: a call inside the module never leaves it, so only a cross-module call
// makes the dispatcher serialize an interface result.
@object()
export class RuntimeIface {
  /** An interface value carrying an engine ID should come back usable. */
  @func()
  @check()
  async fromClientCheck(): Promise<void> {
    const got = await dag
      .runtimeIfaceDep()
      .fromClient(dag.container().from("alpine:3.21").withExec(["echo", "hi"]))
      .stdout()
    if (got.trim() !== "hi") {
      throw new Error(`expected the container's output, got: ${got}`)
    }
  }

  /** A plain object has no engine identity, so this one is meant to fail. */
  @func()
  @check()
  async plainCheck(): Promise<void> {
    await dag.runtimeIfaceDep().plain().stdout()
  }
}
