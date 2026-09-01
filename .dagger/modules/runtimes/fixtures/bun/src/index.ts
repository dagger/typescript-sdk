import { object, func } from "@dagger.io/dagger"

@object()
export class RuntimeBun {
  @func()
  greet(name: string): string {
    return "hello " + name + " from bun"
  }
}
