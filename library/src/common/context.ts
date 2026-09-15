import { GraphQLClient } from "graphql-request"

import { computeQuery, QueryTree } from "./graphql/compute_query.js"
import { globalConnection } from "./graphql/connection.js"

/**
 * A module a generated client serves into the session before its first query.
 * `key` memoizes the serve per session (the module's ref or path); `run`
 * performs it, through a context that carries no serve of its own so it cannot
 * recurse.
 */
export type ServeSpec = {
  key: string
  run: () => Promise<void>
}

export class Context {
  constructor(
    private _queryTree: QueryTree[] = [],
    private _connection = globalConnection,
    // Carried by a generated module client's root context and propagated to
    // every context derived from it, so any query originating from that client
    // serves the client's module first. Undefined for the core client and for
    // the serve itself.
    private _serve?: ServeSpec,
  ) {}

  getGQLClient(): GraphQLClient {
    return this._connection.getGQLClient()
  }

  copy(): Context {
    return new Context([], this._connection, this._serve)
  }

  select(operation: string, args?: Record<string, unknown>): Context {
    return new Context(
      [...this._queryTree, { operation, args }],
      this._connection,
      this._serve,
    )
  }

  /**
   * Select via node(id:) with an inline fragment on the given type.
   * Produces: node(id: "...") { ... on TypeName { children } }
   */
  selectNode(id: string, typeName: string): Context {
    return new Context(
      [
        ...this._queryTree,
        { operation: "node", args: { id }, inlineType: typeName },
      ],
      this._connection,
      this._serve,
    )
  }

  /**
   * Return a copy of this context that serves `spec`'s module before the first
   * query on it (or on any context derived from it) runs. Used by a generated
   * module client to bind its own module to its `dag`.
   */
  withServe(spec: ServeSpec): Context {
    return new Context(this._queryTree, this._connection, spec)
  }

  execute<T>(): Promise<T> {
    if (!this._serve) {
      return computeQuery(this._queryTree, this._connection.getGQLClient())
    }
    return this._connection
      .ensureServed(this._serve.key, this._serve.run)
      .then(() => computeQuery(this._queryTree, this._connection.getGQLClient()))
  }
}

/**
 * Common base class for every generated API class (Client, Container, and
 * dependency-contributed types).
 *
 * It lives here in the SDK runtime rather than in the generated client.gen.ts
 * so that per-dependency generated files (e.g. hello.gen.ts) can `extends
 * BaseClient` without importing a value from client.gen.ts — client.gen.ts
 * `export *`s those dep files, so a value import would create an ESM cycle.
 * client.gen.ts re-exports BaseClient to keep `import { BaseClient } from
 * "./client.gen.js"` working for existing consumers.
 */
export class BaseClient {
  /**
   * @hidden
   */
  constructor(protected _ctx: Context = new Context()) {}
}
