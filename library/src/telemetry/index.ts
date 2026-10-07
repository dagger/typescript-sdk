/**
 * The `@dagger.io/telemetry` entry point.
 *
 * `getTracer` is what a module author uses. `withTracing` is what the facade
 * uses to own the tracer's lifetime around a session — the lifecycle functions
 * it composes stay unexported, because nothing outside needs to start and stop
 * the tracer by hand.
 */
export { getTracer } from "./telemetry.js"
export { withTracing } from "./tracing.js"
