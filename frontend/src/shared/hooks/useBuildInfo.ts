import { useEffect, useState } from "react";

import { api, type BuildInfo } from "../api/client";

// Module-level cache so the binding is called at most once per session.
let cache: BuildInfo | null = null;

/**
 * useBuildInfo fetches the client/protocol versions and build id from the Go
 * backend (cached for the session). Returns null until the first fetch
 * resolves, so callers should fall back to a placeholder while loading.
 */
export function useBuildInfo(): BuildInfo | null {
  const [info, setInfo] = useState<BuildInfo | null>(cache);

  useEffect(() => {
    if (cache) return;
    let active = true;
    api
      .getBuildInfo()
      .then((b) => {
        cache = b;
        if (active) setInfo(b);
      })
      .catch(() => {
        /* version display falls back to placeholder */
      });
    return () => {
      active = false;
    };
  }, []);

  return info;
}
