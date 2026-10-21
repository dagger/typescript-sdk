// Stand-in for the generated core bindings: the type the fixture's source
// shadows, plus one the source really imports.
export class Volume {
  async name(): Promise<string> {
    return ""
  }
}

export class Directory {
  async entries(): Promise<string[]> {
    return []
  }
}
