import { func, object } from "@dagger.io/dagger"

// An undecorated helper named after a core type. Nothing registers it, so the
// generated `class Volume` answered the reference below and the module silently
// took a core Volume — the original failure, one decorator short.
class Volume {
  level = "loud"
}

@object()
export class ShadowedCoreType {
  @func()
  shout(volume: Volume): string {
    return volume.level
  }
}
