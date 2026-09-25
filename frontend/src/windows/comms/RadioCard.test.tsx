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
    useRadios.setState({ radios: {}, selectedRadioId: 0, heldPTT: new Set() });
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

    for (const key of "119250") fireEvent.keyDown(lcd, { key });
    fireEvent.keyDown(lcd, { key: "Enter" });

    expect(api.updateRadioInfo).toHaveBeenCalledWith({
      muted: false,
      radios: [
        { id: 1, name: "Fleet", frequency: Math.fround(119250 / 1000), enabled: true, is_intercom: false },
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

  it("does not select the card when clicking the name input, toggles, or LCD", () => {
    const radio = { id: 3, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    const { container } = render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    fireEvent.click(screen.getByLabelText(/radio name/i));
    fireEvent.click(container.querySelector(".lcd-screen")!);
    expect(api.selectRadio).not.toHaveBeenCalled();
    expect(useRadios.getState().selectedRadioId).toBe(0);
  });

  it("PTT reflects live transmit state from radio.<id>.ptt", () => {
    const radio = { id: 2, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const ptt = screen.getByRole("button", { name: /push-to-talk|transmit/i });
    expect(ptt).not.toBeDisabled();
    expect(ptt).toHaveTextContent(/push-to-talk/i);

    act(() => useRadios.getState().setPTTHeld("radio.2.ptt", true));
    expect(ptt).toHaveTextContent(/transmit/i);
    expect(ptt.className).toContain("keyed");

    act(() => useRadios.getState().setPTTHeld("radio.2.ptt", false));
    expect(ptt).toHaveTextContent(/push-to-talk/i);
  });

  it("PTT reflects global.ptt only while this radio is selected", () => {
    const radio = { id: 4, name: "Fleet", frequency: 118.5, enabled: true, is_intercom: false };
    render(<RadioCard radio={radio} allRadios={[radio]} muted={false} />);
    const ptt = screen.getByRole("button", { name: /push-to-talk|transmit/i });

    act(() => useRadios.getState().setPTTHeld("global.ptt", true));
    expect(ptt).toHaveTextContent(/push-to-talk/i); // not selected yet

    act(() => useRadios.getState().setSelectedRadioId(4));
    expect(ptt).toHaveTextContent(/transmit/i);
  });
});
