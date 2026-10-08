import { dag, object, func } from "@dagger.io/dagger"

// The pre-1.0 spelling: reach a dependency through the core `dag` rather than
// through its own client. Resolved at module scope deliberately, not for
// illustration — a `dag.<dep>()` that is no longer a function throws here, while
// the same call inside a function body only throws when that function is
// dispatched, which no check can reach. This is what makes the compat shim
// load-bearing for `runtimes:dag-dependency-runs`.
void dag.runtimeDep()

@object()
export class RuntimeDagDep {
  @func()
  async viaDag(): Promise<string> {
    return await dag.runtimeDep().value()
  }
}
