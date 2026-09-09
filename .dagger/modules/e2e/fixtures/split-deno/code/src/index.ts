import { object, func } from "@dagger.io/dagger"

@object()
export class SplitDenoApp {
  @func()
  hello(): string {
    return "hello from a split deno module"
  }
}
