import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("../../../shared/api/client", () => ({
  api: { connect: vi.fn().mockResolvedValue(undefined) },
}));
import { api } from "../../../shared/api/client";
import { Welcome } from "./Welcome";

describe("Welcome (guest)", () => {
  beforeEach(() => vi.clearAllMocks());

  it("submits guest credentials via api.connect", async () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "localhost:5002" } });
    fireEvent.change(screen.getByLabelText(/Server Password/i), { target: { value: "pw" } });
    fireEvent.change(screen.getByLabelText(/Player Name/i), { target: { value: "Spacer" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    await waitFor(() =>
      expect(api.connect).toHaveBeenCalledWith("localhost:5002", "Spacer", "pw", ""),
    );
  });

  it("shows an inline error when connect rejects", async () => {
    (api.connect as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("login rejected"));
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    expect(await screen.findByText(/login rejected/i)).toBeInTheDocument();
  });
});
