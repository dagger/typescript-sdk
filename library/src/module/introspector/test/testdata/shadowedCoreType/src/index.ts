import { Directory, func, object } from "@dagger.io/dagger"

// A set of allowed values written as a union, named after a core type. The
// generated client declares `class Volume`, and the scan used to answer this
// reference with it — so the module silently took a core Volume.
export type Volume = "loud" | "quiet"

@object()
export class ShadowedCoreType {
  // The reference that must keep resolving to the core object.
  @func()
  async list(dir: Directory): Promise<string[]> {
    return dir.entries()
  }

  @func()
  shout(volume: Volume): string {
    return volume
  }
}
