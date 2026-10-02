// Stand-in for a per-module client, imported as `@dagger.io/dep`.
export class Dep {
  async greet(): Promise<string> {
    return ""
  }
}
