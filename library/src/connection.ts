import * as opentelemetry from "@opentelemetry/api"

import { ConnectOpts } from "./session/connectOpts.js"
import { withSession } from "./session/connect.js"
import * as telemetry from "./telemetry/telemetry.js"

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
  try {
    telemetry.initialize()

    // Wrap connection into the opentelemetry context for propagation
    await opentelemetry.context.with(telemetry.getContext(), async () => {
      await withSession(fct, cfg)
    })
  } finally {
    await telemetry.close()
  }
}
