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
  setForGuid: (guid: string, info: RadioInfoDTO) => void;
  replaceAll: (all: Record<string, RadioInfoDTO>) => void;
  setSelectedRadioId: (id: number) => void;
  setPTTHeld: (actionId: string, held: boolean) => void;
}

export const useRadios = create<RadiosState>((set) => ({
  radios: {},
  selectedRadioId: 0,
  heldPTT: new Set(),
  setForGuid: (guid, info) => set((s) => ({ radios: { ...s.radios, [guid]: info } })),
  replaceAll: (radios) => set({ radios }),
  setSelectedRadioId: (selectedRadioId) => set({ selectedRadioId }),
  setPTTHeld: (actionId, held) =>
    set((s) => {
      const next = new Set(s.heldPTT);
      if (held) next.add(actionId);
      else next.delete(actionId);
      return { heldPTT: next };
    }),
}));
