import { ConnectOpts } from "./session/connectOpts.js"
import { withSession } from "./session/connect.js"
import { withTracing } from "./telemetry/tracing.js"

/**
 * connection executes the given function using the default global Dagger client.
 *
 * @example
 * ```ts
 * await connection(
 *   async () => {
 *     await dag
 *       .container()
 *       .from("alpine")
 *       .withExec(["apk", "add", "curl"])
 *       .withExec(["curl", "https://dagger.io/"])
 *       .sync()
 *   }, { LogOutput: process.stderr }
 * )
 * ```
 */
export async function connection(
  fct: () => Promise<void>,
  cfg: ConnectOpts = {},
) {
  await withTracing(() => withSession(fct, cfg))
}
