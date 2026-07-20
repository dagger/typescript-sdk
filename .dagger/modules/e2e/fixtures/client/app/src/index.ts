import { dag, object, func } from "@dagger.io/dagger"

/**
 * A module that binds a generated client and depends on client-dep (used
 * internally, so a client for this module is served with the dependency).
 */
@object()
export class ClientApp {
  /**
   * Greet through the dependency module.
   */
  @func()
  async hello(name: string): Promise<string> {
    return await dag.clientDep().greet(name)
  }
}
