import { object, func } from "@dagger.io/dagger"

@object()
export class ConfigConfiguredDeno {
  @func()
  hello(): string {
    return "hello"
  }
}
