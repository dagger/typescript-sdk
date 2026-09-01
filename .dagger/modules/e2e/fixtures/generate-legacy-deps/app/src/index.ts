import { object, func } from "@dagger.io/dagger"

@object()
export class LegacyDepsApp {
  @func()
  hello(): string {
    return "hello"
  }
}
