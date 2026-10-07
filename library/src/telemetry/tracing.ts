import * as opentelemetry from "@opentelemetry/api"

import { close, getContext, initialize } from "./telemetry.js"

/**
 * withTracing runs `fct` with the tracer initialized and the OpenTelemetry
 * context propagated, and shuts the tracer down afterwards.
 *
 * The tracer's lifetime lives here rather than in whatever establishes a
 * session: a session that knows about telemetry puts the OpenTelemetry SDK in
 * every consumer's closure, which is most of what the package split sheds. See
 * design/package-split.md.
 */
export async function withTracing(fct: () => Promise<void>): Promise<void> {
  try {
    initialize()

    await opentelemetry.context.with(getContext(), fct)
  } finally {
    await close()
  }
}
