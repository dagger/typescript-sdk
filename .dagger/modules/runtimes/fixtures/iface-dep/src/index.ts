import { Container, object, func } from "@dagger.io/dagger"

/**
 * Shaped after Container's stdout field, so a core object implements it.
 *
 * An interface value crosses the boundary as the engine ID of the object
 * implementing it, so the only values that can be returned are ones carrying
 * such an ID. Container is the cheapest one to get hold of.
 */
export interface Outputter {
  stdout(): Promise<string>
}

@object()
export class RuntimeIfaceDep {
  /** Returns an interface value that carries an engine ID. */
  @func()
  fromClient(ctr: Container): Outputter {
    return ctr
  }

  /**
   * Returns a plain object implementing the interface, which has no engine
   * identity — the shape dagger/typescript-sdk#90 reported.
   */
  @func()
  plain(): Outputter {
    return { stdout: async () => "plain" }
  }
}
