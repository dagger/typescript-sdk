import { object, func, check } from "@dagger.io/dagger"

export interface Outputter {
  stdout(): Promise<string>
}

/**
 * Carries an interface value in its state, set to a plain object.
 *
 * The same serialization as an interface-typed return, reached the one way an
 * installed module can be made to serialize anything on its own: the engine
 * dispatches the constructor before any call on the object, and that is what
 * turns the state into the engine's view of it.
 */
@object()
export class RuntimeIfaceField {
  @func()
  out: Outputter

  constructor() {
    this.out = { stdout: async () => "plain" }
  }

  /** Any call constructs the object first, which is the point. */
  @func()
  @check()
  fieldCheck(): void {}
}
