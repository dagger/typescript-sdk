import { object, func } from "@dagger.io/dagger"

@object()
export class ConfigApp {
  @func()
  hello(): string {
    return "hello"
  }
}
