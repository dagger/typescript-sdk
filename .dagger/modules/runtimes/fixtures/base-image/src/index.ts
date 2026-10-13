import { object, func, check } from "@dagger.io/dagger"

const pinnedMajor = "22"

@object()
export class RuntimeBaseImage {
  /**
   * Passes only when the call ran on the image package.json pins, which is a
   * major version behind the one the SDK pins by default.
   */
  @func()
  @check()
  nodeMajorCheck(): void {
    const major = process.versions.node.split(".")[0]
    if (major !== pinnedMajor) {
      throw new Error(
        `expected node ${pinnedMajor} from the pinned base image, got ${process.version}`,
      )
    }
  }
}
