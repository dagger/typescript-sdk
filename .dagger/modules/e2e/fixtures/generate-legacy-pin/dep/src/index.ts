import { object, func } from "@dagger.io/dagger"

@object()
export class Legacypindep {
  @func()
  value(): string {
    return "dep"
  }
}
