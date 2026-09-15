{{- define "entrypoint_dispatch_v2" -}}
{{ template "entrypoint_invoke" . }}

type CallRequest = {
  receiverType: string
  receiverValue: string | null
  fnName: string
  fnArgs: string
}

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

async function engineCall(): Promise<void> {
  const { receiverType, fnName, parentJson, args } = decodeRequest(await readStdin())

  await connection(
    async () => {
      // No serve here: each generated client serves its own module before the
      // first query that reaches it — a `dag.<module>()` call through the
      // client's served context, or a received value loaded through the
      // entrypoint loader's served context.
      const result = await invoke(receiverType, fnName, parentJson, args)
      process.stdout.write(
        result === undefined || result === null ? "null" : JSON.stringify(result),
      )
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
