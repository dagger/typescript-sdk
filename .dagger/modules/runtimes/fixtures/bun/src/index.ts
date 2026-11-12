import { object, func, Container } from "@dagger.io/dagger"

@object()
export class RuntimeBun {
  @func()
  greet(name: string): string {
    return "hello " + name + " from bun"
  }

  /**
   * Throws a new nonce each time it runs, so a caller can tell a call that ran
   * again from a failure replayed from the cache.
   */
  @func()
  nonceFailure(): Container {
    throw new Error("RuntimeBun nonce: " + crypto.randomUUID())
  }
}
