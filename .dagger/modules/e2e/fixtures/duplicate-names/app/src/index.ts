import { object, func } from "@dagger.io/dagger"

@object()
export class DuplicateNamesApp {
  @func()
  one(): string {
    return "one"
  }
}
