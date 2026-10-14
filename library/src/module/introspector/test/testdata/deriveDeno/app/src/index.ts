import { func, object } from "@dagger.io/dagger"

import { greet } from "@shared/greet"

@object()
export class DeriveDeno {
  @func()
  hello(name: string): string {
    return greet(name)
  }
}
