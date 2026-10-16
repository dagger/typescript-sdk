// A second hop: reached only through this file, so the derivation has to
// follow it or the module will not compile in its container.
import { punctuate } from "../deeper/punctuate.js"

export function greet(name: string): string {
  return punctuate(`hello ${name} from ../shared`)
}
