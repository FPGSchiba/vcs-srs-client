import { describe, it, expect } from "vitest";
import { useSession } from "./store/session";
import { useWindows } from "./store/windows";

const components = Object.keys(import.meta.glob("./components/*.tsx"));

describe("dead code stays deleted", () => {
  it.each(["Placeholder.tsx", "Pill.tsx", "StatusPill.tsx"])("%s is gone", (f) => {
    expect(components).not.toContain(`./components/${f}`);
  });
  it("stores expose no write-only members", () => {
    const s = useSession.getState();
    const w = useWindows.getState();
    // @ts-expect-error removed in CL4 (FE10)
    void s.server;
    // @ts-expect-error removed in CL4 (FE10)
    void s.error;
    // @ts-expect-error removed in CL4 (FE10)
    void w.isOpen;
    // One assertion per key: a combined arrayContaining check would pass as
    // soon as any single key was gone.
    for (const k of ["server", "error", "setServer", "setError"]) {
      expect(s).not.toHaveProperty(k);
    }
    expect(w).not.toHaveProperty("isOpen");
  });
});
