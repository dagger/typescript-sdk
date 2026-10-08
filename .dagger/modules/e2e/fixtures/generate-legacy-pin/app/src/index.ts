import { object, func } from "@dagger.io/dagger"

@object()
export class LegacyPinApp {
  @func()
  hello(): string {
    return "hello"
  }
}
