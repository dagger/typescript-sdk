/** Used in a @func signature, so the scan has to resolve this file too. */
export type Greeting = string

export function greeting(loud: boolean): Greeting {
  return loud ? "HELLO FROM SHARED" : "hello from shared"
}
