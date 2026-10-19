import { func, object } from "../../../../decorators.js"

@object()
export class Note {
  @func()
  text: string

  constructor(text: string) {
    this.text = text
  }
}

/**
 * NullableReturn class
 */
@object()
export class NullableReturn {
  /**
   * An object reference inside a `Promise<>` union — the shape that failed to
   * resolve at all, because the reference was recorded as "Note | null".
   */
  @func()
  async maybeNote(yes: boolean): Promise<Note | null> {
    return yes ? new Note("hi") : null
  }

  /** The same return, written without the Promise. */
  @func()
  maybeNoteSync(yes: boolean): Note | null {
    return yes ? new Note("hi") : null
  }

  @func()
  async maybeUndefinedNote(): Promise<Note | undefined> {
    return undefined
  }

  @func()
  async maybeNotes(): Promise<Note[] | null> {
    return null
  }

  @func()
  async maybeString(): Promise<string | null> {
    return null
  }

  /** A non-nullable return of the same type stays non-nullable. */
  @func()
  async note(): Promise<Note> {
    return new Note("hi")
  }
}
