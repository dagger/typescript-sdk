import { object, func, check } from "@dagger.io/dagger"

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
}
