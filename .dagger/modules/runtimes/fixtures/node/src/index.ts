import { object, func, check, Container } from "@dagger.io/dagger"

@object()
export class RuntimeNode {
  @func()
  greet(name: string): string {
    return "hello " + name + " from node"
  }

  /** Throws, so a caller can see what a thrown error looks like from outside. */
  @func()
  @check()
  failingCheck(): void {
    throw new Error("RuntimeNode failure: boom")
  }

  /**
   * Throws a new nonce each time it runs, so a caller can tell a call that ran
   * again from a failure replayed from the cache.
   */
  @func()
  nonceFailure(): Container {
    throw new Error("RuntimeNode nonce: " + crypto.randomUUID())
  }
}
