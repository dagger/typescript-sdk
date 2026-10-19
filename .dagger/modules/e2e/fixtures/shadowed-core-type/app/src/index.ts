import { object, func } from "@dagger.io/dagger"

// A type named after a core type, which the generated client declares as
// `class Volume`. Module types are namespaced against other modules but not
// against core, so this one used to generate a module taking a core Volume.
export type Volume = "loud" | "quiet"

@object()
export class ShadowedCoreTypeApp {
  @func()
  shout(volume: Volume): string {
    return volume
  }
}
