import { object, func } from "@dagger.io/dagger"

@object()
export class ParentSourceApp {
  @func()
  hello(): string {
    return "hello"
  }
}
