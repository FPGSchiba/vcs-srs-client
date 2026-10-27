import { useEffect, useMemo, useRef } from "react";

/**
 * useThrottledCommit is a leading-plus-trailing throttle for a drag that
 * commits live (Phase 6.9 CL3, FE05 / D2).
 *
 * - `push(v)` sends at once if `intervalMs` has passed since the last send;
 *   otherwise it remembers `v` and sends the newest remembered value when
 *   the interval elapses. A drag therefore commits at most once per interval
 *   and the level is still heard changing during it.
 * - `flush(v)` cancels any trailing send and sends `v` now: the release
 *   commit, so the final value is exactly where the pointer stopped.
 * - Unmount sends a still-pending trailing value rather than dropping it.
 *
 * The returned functions are stable across renders; the latest `send` is
 * always the one called.
 */
export function useThrottledCommit<T>(
  send: (v: T) => void,
  intervalMs = 150,
): { push(v: T): void; flush(v: T): void } {
  const sendRef = useRef(send);
  sendRef.current = send;
  const last = useRef(Number.NEGATIVE_INFINITY);
  const pending = useRef<{ v: T } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const api = useMemo(() => {
    const emit = (v: T) => {
      last.current = Date.now();
      sendRef.current(v);
    };
    const cancel = () => {
      clearTimeout(timer.current);
      timer.current = undefined;
      pending.current = null;
    };
    const fire = () => {
      timer.current = undefined;
      const p = pending.current;
      pending.current = null;
      if (p) emit(p.v);
    };
    return {
      push(v: T) {
        const since = Date.now() - last.current;
        if (timer.current === undefined && since >= intervalMs) {
          emit(v);
          return;
        }
        pending.current = { v };
        if (timer.current === undefined) {
          timer.current = setTimeout(fire, Math.max(0, intervalMs - since));
        }
      },
      flush(v: T) {
        cancel();
        emit(v);
      },
      unmount() {
        const p = pending.current;
        cancel();
        if (p) emit(p.v);
      },
    };
  }, [intervalMs]);

  useEffect(() => () => api.unmount(), [api]);

  return useMemo(() => ({ push: api.push, flush: api.flush }), [api]);
}
