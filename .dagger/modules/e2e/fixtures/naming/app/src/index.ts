/**
 * Declarations whose schema names differ between the engine's naming rules
 * and the legacy strcase rules, called through the module's own interface.
 */
import { Context, object, func, check } from "@dagger.io/dagger"

/**
 * NamingAppHttpFetcher by the strcase rules and NamingAppHTTPFetcher by the
 * engine's, and e2ETest is e2Etest by the first and e2eTest by the second. A
 * call through it selects the interface and its functions by those names.
 * getURL is getUrl by both, which the entrypoints used to send as declared.
 */
export interface HttpFetcher {
  getURL(path: string): Promise<string>
  e2ETest(): Promise<string>
}

@object()
export class HttpClient {
  @func()
  baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  @func()
  async getURL(path: string): Promise<string> {
    return this.baseUrl + path
  }

  @func()
  async e2ETest(): Promise<string> {
    return "e2e"
  }
}

@object()
export class NamingApp {
  @func()
  httpClient(baseUrl: string): HttpClient {
    return new HttpClient(baseUrl)
  }

  /** Calls through the interface, so by the names the engine gave it. */
  @func()
  async fetch(fetcher: HttpFetcher, path: string): Promise<string> {
    return `${await fetcher.e2ETest()} ${await fetcher.getURL(path)}`
  }

  /**
   * Calls fetch through the engine with an HttpClient as the fetcher, so the
   * names the dispatcher predicts are on the wire. The names this query
   * selects (namingApp, httpClient, fetch) are the same by either rules.
   */
  @func()
  async selfCall(): Promise<string> {
    const client = new Context().select("namingApp").select("httpClient", { baseUrl: "https://example.com" })
    return await new Context()
      .select("namingApp")
      .select("fetch", { fetcher: { _ctx: client }, path: "/x" })
      .execute<string>()
  }

  @func()
  @check()
  async selfCallCheck(): Promise<void> {
    const got = await this.selfCall()
    if (got !== "e2e https://example.com/x") {
      throw new Error(`self-call returned ${JSON.stringify(got)}`)
    }
  }
}
