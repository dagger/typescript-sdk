import { object, func } from "@dagger.io/dagger"

@object()
export class Legacydep {
  @func()
  value(): string {
    return "dep"
  }
}
