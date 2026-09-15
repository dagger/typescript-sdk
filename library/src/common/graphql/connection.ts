import { GraphQLClient } from "graphql-request"

/**
 * Wraps the GraphQL client to allow lazy initialization and setting
 * the GQL client of the global Dagger client instance (`dag`).
 */
export class Connection {
  constructor(private _gqlClient?: GraphQLClient) {}

  // Modules already served into this session, memoized by key so a generated
  // client serves its module at most once however many queries it runs. Cleared
  // with the client because a new session serves nothing yet.
  private _served = new Map<string, Promise<void>>()

  resetClient() {
    this._gqlClient = undefined
    this._served.clear()
  }

  setGQLClient(gqlClient: GraphQLClient) {
    this._gqlClient = gqlClient
  }

  getGQLClient(): GraphQLClient {
    if (!this._gqlClient) {
      throw new Error("GraphQL client is not set")
    }

    return this._gqlClient
  }

  /**
   * Run `serve` the first time `key` is seen in this session and remember the
   * result, so a generated client can ensure its module is served before its
   * first query without serving it again on every call.
   */
  ensureServed(key: string, serve: () => Promise<void>): Promise<void> {
    let pending = this._served.get(key)
    if (!pending) {
      pending = serve()
      this._served.set(key, pending)
    }
    return pending
  }
}

export const globalConnection = new Connection()
