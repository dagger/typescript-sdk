import { object, func } from "@dagger.io/dagger"

@object()
export class ConfigBare {
  @func()
  hello(): string {
    return "hello"
  }
}
