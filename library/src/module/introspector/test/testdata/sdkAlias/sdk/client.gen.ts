// Stand-in for the generated core bindings: only the few types the fixture's
// source imports, so the scan has something to resolve `@dagger.io/dagger` to.
export class Directory {
  async entries(): Promise<string[]> {
    return []
  }
}

export class Secret {
  async plaintext(): Promise<string> {
    return ""
  }
}
