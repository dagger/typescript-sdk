import { object, func } from "@dagger.io/dagger"

@object()
export class RuntimeDep {
  @func()
  value(): string {
    return "hello from the dependency"
  }
}
