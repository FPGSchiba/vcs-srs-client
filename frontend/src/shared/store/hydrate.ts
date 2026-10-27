/**
 * Hydrate guards (Phase 6.9 CL3, FE08).
 *
 * Every window hydrates a store with a one-shot fetch and then subscribes to
 * the backend's push events. Without a guard, a push that lands while the
 * fetch is still in flight is overwritten when the (older) fetch result
 * resolves, rolling the store back.
 *
 * Two rules, because the right answer depends on what a push carries:
 *
 * - `hydrateSnapshot`: the push is a full snapshot, at least as new as the
 *   fetch. A push during flight makes the fetch result stale, so it is
 *   dropped.
 * - `hydrateDelta`: the push is a delta (an appended row, one client's
 *   update). Dropping the fetch would leave the store holding only the
 *   delta, so the fetch is retried instead. The retry is bounded: on the
 *   last attempt the result is applied anyway, with a console warning.
 *
 * Both return a handle. Push handlers call `pushed()` BEFORE they write the
 * store; the effect cleanup calls `dispose()` so a late result never lands in
 * an unmounted window.
 */
export interface HydrateHandle {
  pushed(): void;
  dispose(): void;
}

// Calls fetch synchronously (the Promise executor runs in this tick, so
// callers and tests see the request issued in the same tick, as before the
// guard) and turns a synchronous throw into a rejection that the caller's
// onError handler receives.
function start<T>(fetch: () => Promise<T>): Promise<T> {
  return new Promise<T>((resolve) => resolve(fetch()));
}

export function hydrateSnapshot<T>(
  fetch: () => Promise<T>,
  apply: (v: T) => void,
  onError: (e: unknown) => void,
): HydrateHandle {
  let inflight = true;
  let dirty = false;
  let disposed = false;
  start(fetch).then(
    (v) => {
      inflight = false;
      if (!dirty && !disposed) apply(v);
    },
    (e) => {
      inflight = false;
      if (!disposed) onError(e);
    },
  );
  return {
    pushed() {
      if (inflight) dirty = true;
    },
    dispose() {
      disposed = true;
    },
  };
}

export function hydrateDelta<T>(
  fetch: () => Promise<T>,
  apply: (v: T) => void,
  onError: (e: unknown) => void,
  maxAttempts = 3,
): HydrateHandle {
  let inflight = false;
  let dirty = false;
  let disposed = false;
  let attempt = 0;

  const run = () => {
    attempt += 1;
    inflight = true;
    dirty = false;
    start(fetch).then(
      (v) => {
        inflight = false;
        if (disposed) return;
        if (dirty && attempt < maxAttempts) {
          run();
          return;
        }
        if (dirty) {
          console.warn("hydrate: deltas kept racing the snapshot; applied attempt", attempt);
        }
        apply(v);
      },
      (e) => {
        inflight = false;
        if (!disposed) onError(e);
      },
    );
  };
  run();

  return {
    pushed() {
      if (inflight) dirty = true;
    },
    dispose() {
      disposed = true;
    },
  };
}
