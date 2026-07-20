import { object, func } from "@dagger.io/dagger"

@object()
export class GenerateDepsApp {
  @func()
  hello(): string {
    return "hello"
  }
}
