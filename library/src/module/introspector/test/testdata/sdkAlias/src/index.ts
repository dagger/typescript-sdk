import { Directory, Secret, func, object } from "@dagger.io/dagger"
import { Dep } from "@dagger.io/dep"

@object()
export class SdkAlias {
  public readonly source: Directory

  public readonly token: Secret | undefined

  constructor(source: Directory, token: Secret | undefined) {
    this.source = source
    this.token = token
  }

  @func()
  required(dir: Directory): Directory {
    return dir
  }

  @func()
  async optional(dir?: Directory): Promise<string[]> {
    return dir ? dir.entries() : []
  }

  @func()
  async optionalList(dirs?: Directory[]): Promise<number> {
    return dirs?.length ?? 0
  }

  @func()
  async unionWithUndefined(token: Secret | undefined): Promise<string> {
    return token ? token.plaintext() : ""
  }

  @func()
  dependency(dep: Dep): Dep {
    return dep
  }
}
