// One hop out of the module. What this imports is a second hop, which the
// derivation has to follow or the module will not compile in the container.
import { punctuate } from "../deep/punctuate.js"

export type Name = string

export function greet(name: Name): string {
  return punctuate(`hello ${name}`)
}
