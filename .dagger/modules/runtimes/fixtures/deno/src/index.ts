import { object, func } from "@dagger.io/dagger"

@object()
export class RuntimeDeno {
  @func()
  greet(name: string): string {
    return "hello " + name + " from deno"
  }
}
