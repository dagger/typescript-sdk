import { object, func } from "@dagger.io/dagger"

@object()
export class ManagedTomlBetaPin {
  @func()
  hello(): string {
    return "hello"
  }
}
