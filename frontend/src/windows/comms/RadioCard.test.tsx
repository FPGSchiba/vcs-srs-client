import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("../../shared/api/client", () => ({
  api: { updateRadioInfo: vi.fn().mockResolvedValue(undefined) },
}));
import { api } from "../../shared/api/client";
import { RadioCard } from "./RadioCard";

describe("RadioCard", () => {
  beforeEach(() => vi.clearAllMocks());

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
});
