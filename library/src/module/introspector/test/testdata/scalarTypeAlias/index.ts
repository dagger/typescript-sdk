import { func, object } from "../../../../decorators.js"

// The form the scalar branch is for: every core scalar is declared this way in
// client.gen.ts, and that branch is how one referenced in a signature resolves.
// Only the kind is asserted — a name the engine does not know would still fail
// at load, which is why the union error does not suggest this shape.
type Ref = string & { __Ref: never }

// `boolean` is `true | false` inside the compiler, so it reaches the union
// check too — the boolean branch above it is what keeps this a BooleanKind.
type Flag = boolean

@object()
export class ScalarTypeAlias {
  @func()
  pull(ref: Ref, force: Flag): string {
    return force ? `${ref}!` : ref
  }
}
