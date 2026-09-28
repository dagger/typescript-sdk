import { ConnectOpts } from "./connectOpts.js"
import { withGQLClient } from "./graphql/connect.js"
import { globalConnection } from "./graphql/connection.js"

/**
 * withSession establishes a Dagger session, points the global client at it for
 * the duration of `fct`, and tears it down again.
 *
 * This is the session layer's whole job: no tracing, no bindings. `connection`
 * is this plus the tracer's lifetime, and it lives above both.
 */
export async function withSession(
  fct: () => Promise<void>,
  cfg: ConnectOpts = {},
) {
  // resetClient outside withGQLClient, not inside its callback: a session that
  // fails to come up never reaches the callback, and the global client still has
  // to be left the way it was found.
  try {
    await withGQLClient(cfg, async (gqlClient) => {
      // Set the GQL client inside the global dagger client
      globalConnection.setGQLClient(gqlClient)

      await fct()
    })
  } finally {
    globalConnection.resetClient()
  }
}
