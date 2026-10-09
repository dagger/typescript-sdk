/**
 * The `@dagger.io/session` entry point: everything needed to establish a Dagger
 * session and talk to it, and nothing that knows what the API looks like.
 *
 * `core` and `module` reach this through the package specifier rather than a
 * relative path, which is what lets them be published separately. Inside this
 * repo that specifier resolves to this file through `tsconfig.json`'s `paths`,
 * so there is no build step between editing a layer and typechecking its
 * consumers. See design/package-split.md.
 */
export * from "./errors/index.js"

export { Context, BaseClient } from "./context.js"
export type { ServeSpec } from "./context.js"

export type { ConnectOpts } from "./connectOpts.js"
export { withSession } from "./connect.js"

export { Connection, globalConnection } from "./graphql/connection.js"
export { withGQLClient } from "./graphql/connect.js"
export { computeQuery } from "./graphql/compute_query.js"
export type { QueryTree, Metadata } from "./graphql/compute_query.js"

export { shared } from "./shared.js"
