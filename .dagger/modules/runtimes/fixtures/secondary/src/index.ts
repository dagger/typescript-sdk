import { object, func, check } from "@dagger.io/dagger"

/**
 * A value object the main object constructs, with a TypeScript constructor of
 * its own. Only the main object may carry a constructor typedef — a second one
 * makes the engine reject the whole module — but the class is still
 * constructed, by the module's own code.
 */
@object()
export class RuntimeNote {
  @func()
  text: string

  constructor(text: string) {
    this.text = text
  }

  /** Reached through RuntimeSecondary.note, so the value round-trips. */
  @func()
  @check()
  readsBack(): void {
    if (this.text !== "hi") {
      throw new Error(`RuntimeNote lost its text: ${JSON.stringify(this.text)}`)
    }
  }
}

@object()
export class RuntimeSecondary {
  @func()
  note(): RuntimeNote {
    return new RuntimeNote("hi")
  }
}
