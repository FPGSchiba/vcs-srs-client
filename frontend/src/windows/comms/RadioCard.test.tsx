import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RadioCard } from "./RadioCard";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";

const radio: RadioDTO = {
  id: 1,
  name: "Fleet Common",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
} as RadioDTO;

beforeEach(() => {
  vi.restoreAllMocks();
  useRadios.setState({ selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
});

const show = (r: Partial<RadioDTO> = {}, variantId = "vertical") => {
  const full = { ...radio, ...r };
  render(<RadioCard radio={full} allRadios={[full]} muted={false} variantId={variantId} />);
  return full;
};

describe("variant resolution", () => {
  it("takes its box from the named variant", () => {
    show({}, "narrow-v");
    expect(screen.getByRole("option")).toHaveStyle({ width: "150px", height: "124px" });
  });

  // Review Focus #1, end to end.
  it("falls back to the default variant for an id no descriptor defines", () => {
    show({}, "dial-round");
    expect(screen.getByRole("option")).toHaveStyle({ width: "280px", height: "166px" });
  });

  it("shows the MHZ unit and both chips only on the variants that ask", () => {
    show({}, "vertical");
    expect(screen.getByText("MHZ")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /intercom/i })).toBeInTheDocument();
  });

  it("shows only the enabled chip on horizontal", () => {
    show({}, "horizontal");
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: /intercom/i })).toBeNull();
  });

  it("shows no chips and no unit on the narrow variants", () => {
    show({}, "narrow-h");
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByText("MHZ")).toBeNull();
  });
});

describe("the frequency boundary", () => {
  it("renders the DTO's MHz float as integer kHz digits", () => {
    show({ frequency: 118.5 }, "narrow-h"); // unit hidden, so the group is digits only
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("converts an edited kHz value back to the wire float on commit", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const full = show({ frequency: 118.5 });
    const digits = screen.getAllByRole("button", { name: /digit/i });
    fireEvent.wheel(digits[3], { deltaY: -1 }); // 100 kHz up -> 118_600 kHz
    expect(spy).toHaveBeenCalledTimes(1);
    const sent = spy.mock.calls[0][0].radios.find((r) => r.id === full.id)!;
    // Exactly the expression internal/voice/freq.go uses; the server compares
    // this with ==.
    expect(sent.frequency).toBe(Math.fround(118_600 / 1000));
  });
});

describe("the name", () => {
  it("is a label, not a permanent input", () => {
    show();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
  });

  it("becomes an input on double-click and commits on Enter", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    const input = screen.getByRole("textbox", { name: /radio name/i });
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy.mock.calls[0][0].radios[0].name).toBe("Ops");
  });

  it("reverts on Escape without committing", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    const input = screen.getByRole("textbox", { name: /radio name/i });
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(spy).not.toHaveBeenCalled();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
  });

  it("does not select the card while the name is being edited", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    fireEvent.click(screen.getByRole("textbox", { name: /radio name/i }));
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("state", () => {
  it("marks the card disabled when the radio is off", () => {
    show({ enabled: false });
    expect(screen.getByRole("option")).toHaveAttribute("data-disabled", "true");
    expect(screen.getByText("OFF")).toBeInTheDocument();
  });

  it("marks intercom on the frame even where no chip is shown", () => {
    show({ is_intercom: true }, "narrow-v");
    expect(screen.getByRole("option")).toHaveAttribute("data-intercom", "true");
    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("uses the frozen press-time target for global PTT, not live selection", () => {
    useRadios.setState({
      selectedRadioId: 2,
      globalPttTargetId: 1,
      heldPTT: new Set(["global.ptt"]),
    });
    show();
    expect(screen.getByRole("option")).toHaveAttribute("data-tx", "true");
  });

  it("marks transmit for this radio's own PTT action", () => {
    useRadios.setState({ heldPTT: new Set(["radio.1.ptt"]), globalPttTargetId: 0 });
    show();
    expect(screen.getByRole("option")).toHaveAttribute("data-tx", "true");
  });

  it("selects optimistically and tells the backend", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.click(screen.getByRole("option"));
    expect(useRadios.getState().selectedRadioId).toBe(1);
    expect(spy).toHaveBeenCalledWith(1);
  });

  it("does not update the radios store optimistically on an edit", () => {
    vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const before = useRadios.getState().radios;
    show();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(useRadios.getState().radios).toBe(before);
  });
});

describe("ported from the pre-redesign suite", () => {
  it("commits an enabled toggle as a full write-through payload", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const full = show();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(spy).toHaveBeenCalledWith({ muted: false, radios: [{ ...full, enabled: false }] });
  });

  it("selects via keyboard when the card itself has focus", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.keyDown(screen.getByRole("option"), { key: "Enter" });
    expect(spy).toHaveBeenCalledWith(1);
    expect(useRadios.getState().selectedRadioId).toBe(1);
  });

  it("does not select on a Space typed in the name input", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    fireEvent.keyDown(screen.getByRole("textbox", { name: /radio name/i }), { key: " " });
    expect(spy).not.toHaveBeenCalled();
  });

  it("opens the name editor from the keyboard (Enter / F2)", () => {
    show();
    fireEvent.keyDown(screen.getByText("Fleet Common"), { key: "F2" });
    expect(screen.getByRole("textbox", { name: /radio name/i })).toBeInTheDocument();
  });

  it("does not retarget the indicator when another radio is selected mid-transmission", () => {
    const a = { ...radio, id: 5, name: "A" } as RadioDTO;
    const b = { ...radio, id: 6, name: "B" } as RadioDTO;
    render(
      <>
        <RadioCard radio={a} allRadios={[a, b]} muted={false} variantId="vertical" />
        <RadioCard radio={b} allRadios={[a, b]} muted={false} variantId="vertical" />
      </>,
    );
    const [ca, cb] = screen.getAllByRole("option");
    act(() => useRadios.getState().setSelectedRadioId(5));
    act(() => useRadios.getState().setPTTHeld("global.ptt", true));
    expect(ca).toHaveAttribute("data-tx", "true");
    act(() => useRadios.getState().setSelectedRadioId(6));
    expect(ca).toHaveAttribute("data-tx", "true");
    expect(cb).not.toHaveAttribute("data-tx", "true");
  });
});
