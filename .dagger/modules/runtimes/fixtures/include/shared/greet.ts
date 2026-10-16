// A second hop: reached only through this file, so the derivation has to
// follow it or the module will not compile in its container.
import { punctuate } from "../deeper/punctuate.js"

/** Used in a @func signature, so the scan has to resolve this file too. */
export type Greeting = string

export function greeting(loud: boolean): Greeting {
  return punctuate(loud ? "HELLO FROM SHARED" : "hello from shared")
}
