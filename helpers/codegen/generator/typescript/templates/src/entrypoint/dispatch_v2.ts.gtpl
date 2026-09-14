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

// Developer mode: run a function without the engine loading the module at all.
//
//   npx tsx {{ dispatchFileName }} call <FUNCTION> [--arg value ...]
//
// The receiver is built by running its constructor with no arguments, so a
// module whose constructor has required arguments is engine-only for now.
async function developerCall(argv: string[]): Promise<void> {
  const fnName = argv[0]
  if (fnName === undefined) {
    throw new Error("usage: call <FUNCTION> [--arg value ...]")
  }

  const args: Record<string, any> = {}
  for (let i = 1; i < argv.length; i++) {
    const flag = argv[i]
    if (!flag.startsWith("--")) {
      throw new Error(`expected a --flag, got ${JSON.stringify(flag)}`)
    }
    const name = flag.slice(2)
    const next = argv[i + 1]
    // A flag with no value, or followed by another flag, is a boolean.
    if (next === undefined || next.startsWith("--")) {
      args[name] = true
      continue
    }
    try {
      args[name] = JSON.parse(next)
    } catch {
      args[name] = next
    }
    i++
  }

  await connection(
    async () => {
      const parent = await invoke({{ jsString mainObjectName }}, "", null, {})
      const result = await invoke({{ jsString mainObjectName }}, fnName, parent, args)
      const out =
        result === undefined || result === null
          ? "null"
          : typeof result === "string"
            ? result
            : JSON.stringify(result)
      process.stdout.write(out + "\n")
    },
    { LogOutput: process.stderr },
  )
}

async function main(): Promise<void> {
  const [mode, ...rest] = process.argv.slice(2)
  switch (mode) {
    case "engine-call":
      return engineCall()
    case "call":
      return developerCall(rest)
    default:
      throw new Error(`unknown mode ${JSON.stringify(mode ?? "")}; want "engine-call" or "call"`)
  }
}
{{- end -}}
