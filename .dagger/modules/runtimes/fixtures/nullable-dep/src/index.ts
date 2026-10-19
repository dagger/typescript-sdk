import { object, func } from "@dagger.io/dagger"

/** The object a nullable return hands back when it has one. */
@object()
export class RuntimeNullableNote {
  @func()
  text: string = "hi"
}

@object()
export class RuntimeNullableDep {
  /**
   * A nullable scalar return, observable from the caller.
   *
   * Registered non-null, the null this returns reached the caller coerced to
   * an empty string — a wrong value rather than a failure.
   */
  @func()
  async maybeText(yes: boolean): Promise<string | null> {
    return yes ? "hi" : null
  }

  /** A nullable object return — the shape that failed to generate at all. */
  @func()
  async maybeNote(yes: boolean): Promise<RuntimeNullableNote | null> {
    return yes ? new RuntimeNullableNote() : null
  }
}
