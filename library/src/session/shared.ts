const SHARED = Symbol.for("@dagger.io/session.shared")

type Slots = Record<string, unknown>

/**
 * Hold a process-wide singleton on `globalThis` rather than in module scope.
 *
 * Module scope is not process scope. Once the SDK is several packages, a
 * resolver is free to install two copies of the session package — conflicting
 * ranges, a failed hoist, an `npm link` — and each copy evaluates its own
 * module-level `new`. The symptom is silent and points nowhere near
 * node_modules: decorators register into one registry while the dispatcher
 * reads another, or `connect()` populates a connection `dag` cannot see.
 *
 * `Symbol.for` resolves through the cross-realm symbol registry, so duplicate
 * copies of this very file still agree on the slot and share one instance.
 *
 * The first copy to evaluate wins, which means the shape stored here is a
 * compatibility surface between versions that may run side by side: only ever
 * add to it.
 */
export function shared<T>(key: string, create: () => T): T {
  const slots = ((globalThis as Record<symbol, unknown>)[SHARED] ??=
    {} as Slots) as Slots

  if (!(key in slots)) {
    slots[key] = create()
  }

  return slots[key] as T
}
