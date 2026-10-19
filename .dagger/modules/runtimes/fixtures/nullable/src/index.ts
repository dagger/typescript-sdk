import { dag, object, func, check } from "@dagger.io/dagger"

// Calls reach the dependency rather than this module's own functions: a call
// inside a module never leaves it, so only a cross-module call makes the
// engine read a null off the boundary.
@object()
export class RuntimeNullable {
  /** The null a nullable return promises should come back as one. */
  @func()
  @check()
  async absentCheck(): Promise<void> {
    const text = await dag.runtimeNullableDep().maybeText(false)
    if (text !== null && text !== undefined) {
      throw new Error(`expected a null text, got: ${JSON.stringify(text)}`)
    }

    const note = await dag.runtimeNullableDep().maybeNote(false)
    if (note !== null && note !== undefined) {
      throw new Error(`expected a null note, got: ${JSON.stringify(note)}`)
    }
  }

  /** And the value, when there is one — of both a scalar and an object. */
  @func()
  @check()
  async presentCheck(): Promise<void> {
    const text = await dag.runtimeNullableDep().maybeText(true)
    if (text !== "hi") {
      throw new Error(`expected the dependency's text, got: ${text}`)
    }

    // A nullable object return is a solver in the bindings — it resolves to the
    // object or to null rather than chaining — so it is awaited first.
    const note = await dag.runtimeNullableDep().maybeNote(true)
    if (note === null || note === undefined) {
      throw new Error("expected a note, got null")
    }
    const got = await note.text()
    if (got !== "hi") {
      throw new Error(`expected the note's text, got: ${got}`)
    }
  }
}
