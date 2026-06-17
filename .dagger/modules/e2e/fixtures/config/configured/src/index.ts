import { object, func } from "@dagger.io/dagger"

@object()
export class ConfigConfigured {
  @func()
  hello(): string {
    return "hello"
  }
}
