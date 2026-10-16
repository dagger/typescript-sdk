import { func, object } from "@dagger.io/dagger"

@object()
export class RuntimeLegacyDep {
  @func()
  dep(): string {
    return "dep"
  }
}
