import { describe, it, expect, vi, afterEach } from "vitest";
import { hydrateSnapshot, hydrateDelta } from "./hydrate";

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// Lets every pending promise callback run.
const flush = () => new Promise<void>((r) => setTimeout(r, 0));

afterEach(() => {
  vi.restoreAllMocks();
});

describe("hydrateSnapshot", () => {
  it("snapshot: a push during flight drops the hydrate", async () => {
    const d = deferred<number>();
    const apply = vi.fn();
    const h = hydrateSnapshot(() => d.promise, apply, vi.fn());
    h.pushed();
    d.resolve(1);
    await flush();
    expect(apply).not.toHaveBeenCalled();
  });

  it("snapshot: no push means apply(result)", async () => {
    const d = deferred<number>();
    const apply = vi.fn();
    hydrateSnapshot(() => d.promise, apply, vi.fn());
    d.resolve(1);
    await flush();
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith(1);
  });

  it("snapshot: dispose drops a late result", async () => {
    const d = deferred<number>();
    const apply = vi.fn();
    const h = hydrateSnapshot(() => d.promise, apply, vi.fn());
    h.dispose();
    d.resolve(1);
    await flush();
    expect(apply).not.toHaveBeenCalled();
  });
});

describe("hydrateDelta", () => {
  it("delta: a push during flight re-fetches and applies the second result", async () => {
    const d1 = deferred<number>();
    const d2 = deferred<number>();
    const fetch = vi.fn().mockReturnValueOnce(d1.promise).mockReturnValueOnce(d2.promise);
    const apply = vi.fn();
    const h = hydrateDelta(fetch, apply, vi.fn());
    h.pushed();
    d1.resolve(1);
    await flush();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(apply).not.toHaveBeenCalled();
    d2.resolve(2);
    await flush();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith(2);
  });

  it("delta: after 3 racing attempts the third result applies with a warning", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const ds = [deferred<number>(), deferred<number>(), deferred<number>()];
    let i = 0;
    const fetch = vi.fn(() => ds[i++].promise);
    const apply = vi.fn();
    const h = hydrateDelta(fetch, apply, vi.fn());
    for (let n = 0; n < 3; n++) {
      h.pushed();
      ds[n].resolve(n + 1);
      await flush();
    }
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith(3);
    expect(warn).toHaveBeenCalledWith(
      "hydrate: deltas kept racing the snapshot; applied attempt",
      3,
    );
  });

  it("delta: a push after resolve does not re-fetch", async () => {
    const d = deferred<number>();
    const fetch = vi.fn(() => d.promise);
    const apply = vi.fn();
    const h = hydrateDelta(fetch, apply, vi.fn());
    d.resolve(1);
    await flush();
    h.pushed();
    await flush();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledTimes(1);
  });

  it("delta: dispose drops a late result", async () => {
    const d = deferred<number>();
    const apply = vi.fn();
    const h = hydrateDelta(() => d.promise, apply, vi.fn());
    h.dispose();
    d.resolve(1);
    await flush();
    expect(apply).not.toHaveBeenCalled();
  });
});

describe.each([
  ["snapshot", hydrateSnapshot],
  ["delta", hydrateDelta],
] as const)("%s rejection", (_name, hydrate) => {
  it("rejection calls onError once and applies nothing", async () => {
    const d = deferred<number>();
    const fetch = vi.fn(() => d.promise);
    const apply = vi.fn();
    const onError = vi.fn();
    hydrate(fetch, apply, onError);
    const err = new Error("boom");
    d.reject(err);
    await flush();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledWith(err);
    expect(apply).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("a synchronous throw from fetch is routed to onError, not thrown", async () => {
    const err = new Error("sync boom");
    const fetch = vi.fn((): Promise<number> => {
      throw err;
    });
    const apply = vi.fn();
    const onError = vi.fn();
    expect(() => hydrate(fetch, apply, onError)).not.toThrow();
    // fetch is still issued in the same tick.
    expect(fetch).toHaveBeenCalledTimes(1);
    await flush();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledWith(err);
    expect(apply).not.toHaveBeenCalled();
  });
});
