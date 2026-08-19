import { object, func } from "@dagger.io/dagger"

@object()
export class RuntimeNode {
  @func()
  greet(name: string): string {
    return "hello " + name + " from node"
  }
}
