import { create } from "zustand";
import { api } from "../api/client";
import type { ProfileState, ProfileSummary } from "../api/client";

interface ProfileStore {
  state: ProfileState;
  profiles: ProfileSummary[];
  setState: (s: ProfileState) => void;
  setProfiles: (p: ProfileSummary[]) => void;
  /** Re-reads both the directory listing and the active-profile state. */
  refresh: () => Promise<void>;
}

const EMPTY: ProfileState = { active_path: "", active_name: "", dirty: false, dir: "" };

/**
 * Profile state is GO-OWNED: `dirty` is computed backend-side by comparing
 * live config against the active profile's file, and the Comms popout is a
 * separate webview with its own JS heap. This store is a cache of what
 * `profile:state` last delivered, never a source of truth.
 */
export const useProfile = create<ProfileStore>((set) => ({
  state: EMPTY,
  profiles: [],
  setState: (s) => set({ state: s ?? EMPTY }),
  setProfiles: (p) => set({ profiles: p ?? [] }),
  refresh: async () => {
    const [state, profiles] = await Promise.all([
      api.getProfileState(),
      api.listProfiles(),
    ]);
    set({ state: state ?? EMPTY, profiles: profiles ?? [] });
  },
}));
