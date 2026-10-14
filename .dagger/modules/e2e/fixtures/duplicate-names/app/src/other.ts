import { object, func } from "@dagger.io/dagger"

@object()
export class DuplicateNamesApp {
  @func()
  two(): string {
    return "two"
  }
}
