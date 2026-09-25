import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("../../../shared/api/client", () => ({
  api: {
    connect: vi.fn().mockResolvedValue(undefined),
    getBuildInfo: vi.fn().mockResolvedValue({
      client_version: "0.1.0",
      protocol_version: "1.0.0",
      build: "test",
    }),
  },
}));
import { api } from "../../../shared/api/client";
import { useSession } from "../../../shared/store/session";
import { Welcome } from "./Welcome";

describe("Welcome (guest)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset the shared session store so phase changes don't leak between tests.
    useSession.setState({ phase: "welcome", conn: "disconnected", error: null, server: "" });
  });

  it("submits guest credentials via api.connect", async () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "localhost:5002" } });
    fireEvent.change(screen.getByLabelText(/Server Password/i), { target: { value: "pw" } });
    fireEvent.change(screen.getByLabelText(/Player Name/i), { target: { value: "Spacer" } });
    fireEvent.change(screen.getByLabelText(/FFID/i), { target: { value: "VG12" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    await waitFor(() =>
      expect(api.connect).toHaveBeenCalledWith("localhost:5002", "Spacer", "pw", "VG12"),
    );
  });

  it("shows an inline error when connect rejects", async () => {
    (api.connect as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("login rejected"));
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/FFID/i), { target: { value: "VG12" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    expect(await screen.findByText(/login rejected/i)).toBeInTheDocument();
  });

  // The server validates UnitId against ^[A-Z0-9]{2,4}$ and rejects
  // anything else, including an empty string -- a user who leaves the FFID
  // field blank used to get a login failure with no useful explanation.
  it("shows a clear validation error and does not call api.connect when FFID is blank", async () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "localhost:5002" } });
    fireEvent.change(screen.getByLabelText(/Player Name/i), { target: { value: "Spacer" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));

    expect(await screen.findByText(/FFID must be/i)).toBeInTheDocument();
    expect(api.connect).not.toHaveBeenCalled();
  });

  it("shows a clear validation error when FFID does not match the server's pattern", async () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "localhost:5002" } });
    fireEvent.change(screen.getByLabelText(/Player Name/i), { target: { value: "Spacer" } });
    fireEvent.change(screen.getByLabelText(/FFID/i), { target: { value: "way-too-long-and-lowercase" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));

    expect(await screen.findByText(/FFID must be/i)).toBeInTheDocument();
    expect(api.connect).not.toHaveBeenCalled();
  });
});
