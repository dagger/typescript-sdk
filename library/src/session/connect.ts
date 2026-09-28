import * as opentelemetry from "@opentelemetry/api"

import { ConnectOpts } from "./connectOpts.js"
import { withGQLClient } from "./graphql/connect.js"
import { globalConnection } from "./graphql/connection.js"
// Session reaching up into telemetry. Tolerated for now because moving it is a
// behaviour change, not a move: `connection` owns the tracer's lifetime, and
// nothing else is positioned to start and stop it around the session. It has to
// go before telemetry can ship as its own package, or every consumer of a
// session drags the OpenTelemetry SDK in with it — see design/package-split.md.
import * as telemetry from "../telemetry/telemetry.js"

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
      try {
        await withGQLClient(cfg, async (gqlClient) => {
          // Set the GQL client inside the global dagger client
          globalConnection.setGQLClient(gqlClient)

          await fct()
        })
      } finally {
        globalConnection.resetClient()
      }
    })
  } finally {
    await telemetry.close()
  }
}
