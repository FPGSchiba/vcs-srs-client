import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";

vi.mock("../../shared/api/client", () => ({
  api: {
    updateRadioInfo: vi.fn().mockResolvedValue(undefined),
    selectRadio: vi.fn().mockResolvedValue(undefined),
  },
}));
import { api } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";
import { RadioCard } from "./RadioCard";

describe("RadioCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
  });

  it("commits a name edit via api.updateRadioInfo", () => {
    const radio = { id: 1, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const nameInput = screen.getByLabelText(/radio name/i);
    fireEvent.change(nameInput, { target: { value: "Wing" } });
    fireEvent.blur(nameInput);
    expect(api.updateRadioInfo).toHaveBeenCalledWith({
      muted: false,
      radios: [{ id: 1, name: "Wing", frequency: 118.5, enabled: true, is_intercom: false }],
    });
  });

  it("commits an enabled toggle", () => {
    const radio = { id: 1, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    fireEvent.click(screen.getByLabelText(/toggle enabled/i));
    expect(api.updateRadioInfo).toHaveBeenCalledWith({
      muted: false,
      radios: [{ id: 1, name: "Fleet", frequency: 118.5, enabled: false, is_intercom: false }],
    });
  });

  it("commits a frequency edit via the LCD through api.updateRadioInfo", () => {
    const radio = { id: 1, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    const { container } = render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const lcd = container.querySelector(".lcd-screen")!;

    // 119251/1000 = 119.251, NOT exactly representable in float32 (unlike
    // e.g. 119.25), so this exercises Math.fround: an implementation that
    // dropped it would compute a different (float64) value here.
    for (const key of "119251") fireEvent.keyDown(lcd, { key });
    fireEvent.keyDown(lcd, { key: "Enter" });

    expect(api.updateRadioInfo).toHaveBeenCalledWith({
      muted: false,
      radios: [
        { id: 1, name: "Fleet", frequency: Math.fround(119251 / 1000), enabled: true, is_intercom: false },
      ],
    });
  });

  it("selects the card on click, marking it and calling api.selectRadio", () => {
    const radio = { id: 3, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    const { container } = render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const card = container.querySelector(".radio")!;

    expect(card).toHaveAttribute("aria-selected", "false");
    fireEvent.click(card);

    expect(api.selectRadio).toHaveBeenCalledWith(3);
    expect(useRadios.getState().selectedRadioId).toBe(3);
    expect(card).toHaveAttribute("aria-selected", "true");
  });

  it("does not select the card when clicking the name input, a toggle, or the LCD", () => {
    const radio = { id: 3, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    const { container } = render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    fireEvent.click(screen.getByLabelText(/radio name/i));
    fireEvent.click(screen.getByLabelText(/toggle enabled/i));
    fireEvent.click(container.querySelector(".lcd-screen")!);
    expect(api.selectRadio).not.toHaveBeenCalled();
    expect(useRadios.getState().selectedRadioId).toBe(0);
  });

  it("selects the card via keyboard (Enter/Space) when it has focus", () => {
    const radio = { id: 7, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    const { container } = render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const card = container.querySelector(".radio")!;

    fireEvent.keyDown(card, { key: "Enter" });
    expect(api.selectRadio).toHaveBeenCalledWith(7);
    expect(useRadios.getState().selectedRadioId).toBe(7);
  });

  it("does not treat a Space bubbled up from the name input as a card-select", () => {
    const radio = { id: 8, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    fireEvent.keyDown(screen.getByLabelText(/radio name/i), { key: " " });
    expect(api.selectRadio).not.toHaveBeenCalled();
    expect(useRadios.getState().selectedRadioId).toBe(0);
  });

  it("PTT is a disabled live-transmit indicator, not a clickable control", () => {
    const radio = { id: 2, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const ptt = screen.getByRole("button", { name: /push-to-talk|transmit/i });
    // Wiring real click-to-talk needs backend binding surface that doesn't
    // exist yet, so the control stays disabled -- it must never look
    // interactive without being interactive.
    expect(ptt).toBeDisabled();
    expect(ptt).toHaveTextContent(/push-to-talk/i);

    act(() => useRadios.getState().setPTTHeld("radio.2.ptt", true));
    expect(ptt).toHaveTextContent(/transmit/i);
    expect(ptt.className).toContain("keyed");

    act(() => useRadios.getState().setPTTHeld("radio.2.ptt", false));
    expect(ptt).toHaveTextContent(/push-to-talk/i);
  });

  it("PTT reflects global.ptt when this radio was selected at press time (select-then-press)", () => {
    const radio = { id: 4, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const ptt = screen.getByRole("button", { name: /push-to-talk|transmit/i });

    act(() => useRadios.getState().setSelectedRadioId(4));
    expect(ptt).toHaveTextContent(/push-to-talk/i); // not pressed yet

    act(() => useRadios.getState().setPTTHeld("global.ptt", true));
    expect(ptt).toHaveTextContent(/transmit/i);
  });

  // Regression test for the "indicator lies about which radio is live" bug:
  // the backend's resolveTXTarget (internal/app/voice.go) resolves
  // global.ptt against the selected radio ONCE, at press time, and never
  // re-resolves it for the life of the hold. The frontend must freeze the
  // same way -- previously it recomputed the target live on every render,
  // so reselecting mid-transmission made the UI show the NEW radio as
  // transmitting while the backend kept transmitting on the OLD one.
  it("freezes the global.ptt target at press time -- reselecting mid-transmission does not retarget the indicator (press-then-reselect)", () => {
    const radioA = { id: 5, name: "A", frequency: 118.5, enabled: true, is_intercom: false };
    const radioB = { id: 6, name: "B", frequency: 119.5, enabled: true, is_intercom: false };
    render(
      <>
        <RadioCard radio={radioA} allRadios={[radioA, radioB]} muted={false} />
        <RadioCard radio={radioB} allRadios={[radioA, radioB]} muted={false} />
      </>,
    );
    const [pttA, pttB] = screen.getAllByRole("button", { name: /push-to-talk|transmit/i });

    act(() => useRadios.getState().setSelectedRadioId(5));
    act(() => useRadios.getState().setPTTHeld("global.ptt", true));
    expect(pttA).toHaveTextContent(/transmit/i);
    expect(pttB).toHaveTextContent(/push-to-talk/i);

    // Reselect B WHILE still holding global.ptt.
    act(() => useRadios.getState().setSelectedRadioId(6));
    expect(pttA).toHaveTextContent(/transmit/i); // still A -- frozen at press time
    expect(pttB).toHaveTextContent(/push-to-talk/i); // NOT B, despite now being selected

    act(() => useRadios.getState().setPTTHeld("global.ptt", false));
    expect(pttA).toHaveTextContent(/push-to-talk/i);

    // A later press now targets B, since B is selected at THIS press's time.
    act(() => useRadios.getState().setPTTHeld("global.ptt", true));
    expect(pttB).toHaveTextContent(/transmit/i);
    expect(pttA).toHaveTextContent(/push-to-talk/i);
  });
});
