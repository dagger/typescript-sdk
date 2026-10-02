import { object, func, Directory } from "@dagger.io/dagger"

@object()
export class GenerateDepsApp {
  @func()
  hello(): string {
    return "hello"
  }

  // Load-bearing, not illustrative: an optional object-typed argument is the
  // one signature the scan cannot resolve by name alone, so it is what proves
  // the module's `@dagger.io/dagger` import resolved at all. Every generate
  // check runs this fixture through the introspector; without a signature of
  // this shape they all pass against a scan that resolved nothing.
  @func()
  async entries(dir?: Directory): Promise<string[]> {
    return dir ? dir.entries() : []
  }
}
