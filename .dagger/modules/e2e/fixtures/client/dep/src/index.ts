import { object, func } from "@dagger.io/dagger"

/**
 * A dependency module bound clients can pull in.
 */
@object()
export class ClientDep {
  /**
   * Return a friendly greeting from the dependency.
   */
  @func()
  greet(name: string): string {
    return `hello ${name} from client-dep`
  }
}
