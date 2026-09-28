import type { Platform } from "../../../../../core/client.gen.js"
import { func, object } from "../../../../decorators.js"

@object()
export class Scalar {
  @func()
  fromPlatform(platform: Platform): string {
    return platform as string
  }

  @func()
  fromPlatforms(platforms: Platform[]): string[] {
    return platforms.map((p) => p as string)
  }
}
