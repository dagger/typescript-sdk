import { object, func } from "@dagger.io/dagger"

@object()
export class GenerateDenoApp {
  @func()
  hello(): string {
    return "hello from deno"
  }
}
