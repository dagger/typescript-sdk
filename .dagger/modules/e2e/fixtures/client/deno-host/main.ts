// A Deno app, as `deno init` lays one out, that uses a generated client: both
// packages resolve through the deno.json import map, nothing is installed.
import { connection } from "@dagger.io/dagger"
import { clientApp } from "@dagger.io/client-app"

await connection(async () => {
  console.log(await clientApp().hello("deno"))
})
