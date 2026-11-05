import { getBooleanFromEnv } from "@opentelemetry/core"
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto"
import {
  defaultResource,
  resourceFromAttributes,
} from "@opentelemetry/resources"
import {
  BatchSpanProcessor,
  type SpanProcessor,
} from "@opentelemetry/sdk-trace-base"
// NodeTracerProvider rather than sdk-node's NodeSDK: the two things used here
// are a resource and a span processor, and `register()` installs the same
// AsyncLocalStorage context manager and W3C propagator getContext() reads. The
// NodeSDK wrapper additionally pulls sdk-metrics, sdk-logs, instrumentation,
// propagator-jaeger and the gRPC exporter base into bundle/core.js, which every
// module evaluates on every call — measured at ~14 MiB of RSS per dispatcher,
// for signals this SDK never emits.
import { NodeTracerProvider } from "@opentelemetry/sdk-trace-node"

import { LiveProcessor } from "./live_processor.js"

const SERVICE_NAME = "dagger-typescript-sdk"

/*
 * Look for variables prefixed with OTel to see if OpenTelemetry is configured.
 */
function otelConfigured(): boolean {
  return Object.keys(process.env).some((key) => key.startsWith("OTEL_"))
}
/**
 * Batch span processor scheduler delays.
 * We set to 100ms so it's almost live.
 */
const NEARLY_IMMEDIATE = 100

/**
 * A wrapper around the OpenTelemetry SDK to configure it for Dagger.
 */
export class DaggerOtelConfigurator {
  private is_configured = false

  private provider?: NodeTracerProvider

  /**
   * Initialize the Open Telemetry SDK if enabled or not already configured.
   */
  initialize() {
    if (this.is_configured) {
      return
    }
    this.configure()
    this.is_configured = true
  }

  configure() {
    if (!otelConfigured()) {
      return
    }

    if (getBooleanFromEnv("OTEL_SDK_DISABLED") ?? true) {
      return
    }

    this.setupEnv()

    const exporter = new OTLPTraceExporter()

    let processor: SpanProcessor
    if (process.env.OTEL_EXPORTER_OTLP_TRACES_LIVE !== undefined) {
      processor = new LiveProcessor(exporter, {
        scheduledDelayMillis: NEARLY_IMMEDIATE,
      })
    } else {
      processor = new BatchSpanProcessor(exporter, {
        scheduledDelayMillis: NEARLY_IMMEDIATE,
      })
    }

    // Merged onto the default resource rather than replacing it, so the
    // telemetry.sdk.* attributes NodeSDK used to contribute are still reported.
    this.provider = new NodeTracerProvider({
      resource: defaultResource().merge(
        resourceFromAttributes({ "service.name": SERVICE_NAME }),
      ),
      spanProcessors: [processor],
    })

    // Registers the provider, the context manager and the propagator globally.
    this.provider.register()
  }

  /**
   * Shutdown the Open Telemetry SDK to flush traces and close the connection.
   */
  async close() {
    if (this.provider) {
      await this.provider.shutdown()
    }
  }

  /**
   * Setup environment for auto-configuring the SDK.
   */
  setupEnv() {
    // TODO: The insecure flag should be set by the shim instead of the SDK.
    Object.entries(process.env).forEach(([key, value]) => {
      if (
        key.startsWith("OTEL_") &&
        key.endsWith("_ENDPOINT") &&
        value?.startsWith("http://")
      ) {
        const insecure = key.replace(/_ENDPOINT$/, "_INSECURE")
        if (process.env[insecure] === undefined) {
          process.env[insecure] = "true"
        }
      }
    })
  }
}
