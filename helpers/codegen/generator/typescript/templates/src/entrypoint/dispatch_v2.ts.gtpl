{{- define "entrypoint_dispatch_v2" -}}
{{ template "entrypoint_invoke" . }}

type CallRequest = {
  receiverType: string
  receiverValue: string | null
  fnName: string
  fnArgs: string
}

// The reply the Dang entrypoint decodes from stdout, one shape or the other.
// `result` is the function's JSON result as a *string*, for the same reason
// receiverValue and fnArgs arrive as strings: the entrypoint materializes it into
// a JSON scalar, which decodes from a string, and hands it to the engine
// verbatim. `error` is what the function threw, carried as data rather than as a
// nonzero exit so the entrypoint can raise it as the function's own error — an
// exit code reaches the caller as "exit code: 1" with the message buried in the
// exec log. A nonzero exit therefore means the dispatcher itself failed.
type CallReply = { result: string } | { error: { message: string } }

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = []
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.from(chunk))
  }
  return Buffer.concat(chunks).toString("utf8")
}

// receiverValue and fnArgs arrive as JSON *strings*, not as embedded objects: the
// Dang entrypoint builds the request with JSON.encode, which serializes a JSON
// scalar to a string. So each is decoded a second time here.
function decodeRequest(raw: string): {
  receiverType: string
  fnName: string
  parentJson: any
  args: Record<string, any>
} {
  const request = JSON.parse(raw) as CallRequest
  return {
    receiverType: request.receiverType,
    fnName: request.fnName,
    parentJson:
      request.receiverValue === null || request.receiverValue === undefined
        ? null
        : JSON.parse(request.receiverValue),
    args: JSON.parse(request.fnArgs ?? "{}") as Record<string, any>,
  }
}

// Only the message crosses the boundary. The builtin runtime's error channel
// carried a typed error's extensions as values; the entrypoint can raise nothing
// richer than a message, so they stop here. The stack stays in the exec log.
function errorMessage(e: unknown): string {
  if (e instanceof Error) {
    return e.message
  }
  try {
    return JSON.stringify(e) ?? String(e)
  } catch {
    return String(e)
  }
}

async function engineCall(): Promise<void> {
  const { receiverType, fnName, parentJson, args } = decodeRequest(await readStdin())

  await connection(
    async () => {
      // No serve here: a module is only ever reached through a `dag.<module>()`
      // call, and each generated client serves its own module before its first
      // query.
      let reply: CallReply
      try {
        const result = await invoke(receiverType, fnName, parentJson, args)
        reply = {
          result: result === undefined || result === null ? "null" : JSON.stringify(result),
        }
      } catch (e: unknown) {
        console.error(e)
        reply = { error: { message: errorMessage(e) } }
      }
      process.stdout.write(JSON.stringify(reply))
    },
    // stdout is the result channel now, so the session's logs go to stderr. The
    // engine reads one and streams the other; crossing them corrupts the result.
    { LogOutput: process.stderr },
  )
}

// The mode is checked rather than ignored so that a stale entrypoint — one
// generated against a different protocol — fails here with something readable
// instead of hanging on a stdin that never arrives.
async function main(): Promise<void> {
  const mode = process.argv[2]
  if (mode !== "engine-call") {
    throw new Error(`unknown mode ${JSON.stringify(mode ?? "")}; want "engine-call"`)
  }
  return engineCall()
}
{{- end -}}
