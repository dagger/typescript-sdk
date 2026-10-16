// A second hop, reached only through this file — which the import map is the
// only thing pointing at.
import { punctuate } from "../deeper/punctuate.js"

export function greet(name: string): string {
  return punctuate(`hello ${name}`)
}
