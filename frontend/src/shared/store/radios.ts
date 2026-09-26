import { create } from "zustand";
import type { RadioInfoDTO } from "../api/client";

interface RadiosState {
  radios: Record<string, RadioInfoDTO>;
  /** The radio id `global.ptt` currently targets, 0 if none -- mirrors
   * VoiceStateDTO.selected_radio (see App.VoiceState / App.SelectRadio).
   * Selection is purely client-local (a.st.SelectedRadio on the backend has
   * no server echo), so this is updated optimistically on click and
   * re-synced from api.voiceState() on mount. */
  selectedRadioId: number;
  /** The set of keybind action ids (e.g. "global.ptt", "radio.3.ptt")
   * currently held down, driven by hotkey:pressed / hotkey:released. Used
   * to derive each RadioCard's live transmit state. */
  heldPTT: Set<string>;
  /** The radio id `global.ptt` is transmitting to for the CURRENT hold, 0
   * if `global.ptt` is not held. Captured once, at the moment `global.ptt`
   * transitions to held, from whatever `selectedRadioId` is at that
   * instant -- mirroring the backend's `resolveTXTarget` (internal/app/voice.go),
   * which resolves `global.ptt` against the selected radio at press time
   * and deliberately never re-resolves it for the life of the hold. If the
   * indicator instead recomputed the target live from `selectedRadioId` on
   * every render, selecting a different radio mid-transmission would make
   * the UI show the NEW radio as transmitting while the backend keeps
   * transmitting on the OLD one -- an indicator that actively lies about
   * which radio is live. */
  globalPttTargetId: number;
  setForGuid: (guid: string, info: RadioInfoDTO) => void;
  replaceAll: (all: Record<string, RadioInfoDTO>) => void;
  setSelectedRadioId: (id: number) => void;
  setPTTHeld: (actionId: string, held: boolean) => void;
}

export const useRadios = create<RadiosState>((set) => ({
  radios: {},
  selectedRadioId: 0,
  heldPTT: new Set(),
  globalPttTargetId: 0,
  setForGuid: (guid, info) => set((s) => ({ radios: { ...s.radios, [guid]: info } })),
  replaceAll: (radios) => set({ radios }),
  setSelectedRadioId: (selectedRadioId) => set({ selectedRadioId }),
  setPTTHeld: (actionId, held) =>
    set((s) => {
      const next = new Set(s.heldPTT);
      const wasHeld = next.has(actionId);
      if (held) next.add(actionId);
      else next.delete(actionId);

      if (actionId !== "global.ptt") return { heldPTT: next };

      // Freeze (on the true press-edge) / clear (on release) the
      // global.ptt target. Ignore a redundant "pressed" while it is
      // already held so an upstream repeat event can't re-capture a new
      // target mid-hold.
      if (held && !wasHeld) {
        return { heldPTT: next, globalPttTargetId: s.selectedRadioId };
      }
      if (!held) {
        return { heldPTT: next, globalPttTargetId: 0 };
      }
      return { heldPTT: next };
    }),
}));
