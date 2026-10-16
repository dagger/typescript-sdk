import { func, object } from "@dagger.io/dagger"

import { x } from "@x/index"

@object()
export class DeriveBaseUrl {
  @func()
  value(): number {
    return x
  }
}
