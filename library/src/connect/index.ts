/**
 * The `@dagger.io/connect` entry point.
 *
 * One function, and its own package because of what it composes: a session from
 * `@dagger.io/session` and the tracer's lifetime from `@dagger.io/telemetry`.
 * It sits above both rather than in either, which is what keeps the
 * OpenTelemetry SDK out of a bare client's closure — and what stopped it from
 * living in a layer when the facade was the only thing above them.
 */
export { connection } from "./connection.js"
