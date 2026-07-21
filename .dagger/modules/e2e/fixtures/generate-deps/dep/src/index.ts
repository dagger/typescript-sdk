import { object, func } from "@dagger.io/dagger"

@object()
export class Gendep {
  @func()
  value(): string {
    return "dep"
  }
}
