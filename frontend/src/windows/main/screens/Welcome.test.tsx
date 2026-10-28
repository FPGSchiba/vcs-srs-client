import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

vi.mock("../../../shared/api/client", () => ({
  api: {
    connect: vi.fn().mockResolvedValue(undefined),
    cancelConnect: vi.fn().mockResolvedValue(undefined),
    getLastServerAddress: vi.fn().mockResolvedValue(""),
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
    useSession.setState({ phase: "welcome", conn: "disconnected", sessionNotice: null });
    (api.getLastServerAddress as ReturnType<typeof vi.fn>).mockResolvedValue("");
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

const mocked = (f: unknown) => f as ReturnType<typeof vi.fn>;

function fillManual() {
  fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
  fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "srv.example:5002" } });
  fireEvent.change(screen.getByLabelText(/Server Password/i), { target: { value: "pw" } });
  fireEvent.change(screen.getByLabelText(/Player Name/i), { target: { value: "Spacer" } });
  fireEvent.change(screen.getByLabelText(/FFID/i), { target: { value: "VG12" } });
}

describe("Welcome (Phase 6.9 CL1)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useSession.setState({ phase: "welcome", conn: "disconnected", sessionNotice: null });
    mocked(api.getLastServerAddress).mockResolvedValue("");
    mocked(api.connect).mockResolvedValue(undefined);
  });

  it("Enter in the password field submits once", async () => {
    render(<Welcome />);
    fillManual();
    fireEvent.submit(screen.getByLabelText(/Server Password/i).closest("form")!);
    await waitFor(() => expect(api.connect).toHaveBeenCalledTimes(1));
    expect(api.connect).toHaveBeenCalledWith("srv.example:5002", "Spacer", "pw", "VG12");
  });

  it("BACK does not submit", () => {
    render(<Welcome />);
    fillManual();
    const back = screen.getByText(/^BACK$/);
    expect(back).toHaveAttribute("type", "button");
    fireEvent.click(back);
    expect(screen.getByText(/JOIN AS GUEST/i)).toBeInTheDocument();
    expect(api.connect).not.toHaveBeenCalled();
  });

  it("error slot is an alert", async () => {
    mocked(api.connect).mockRejectedValueOnce(new Error("login rejected"));
    render(<Welcome />);
    fillManual();
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/login rejected/i);
  });

  it("CANCEL aborts and returns to the filled form with no error", async () => {
    let reject: (e: unknown) => void = () => {};
    mocked(api.connect).mockImplementation(
      () => new Promise((_, r) => { reject = r; }),
    );
    mocked(api.cancelConnect).mockImplementation(async () => {
      reject(new Error(JSON.stringify({ message: "connect cancelled" })));
    });
    render(<Welcome />);
    fillManual();
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    fireEvent.click(await screen.findByText(/^CANCEL$/));

    await waitFor(() => expect(screen.getByLabelText(/Server Address/i)).toBeInTheDocument());
    expect(api.cancelConnect).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText(/Server Address/i)).toHaveValue("srv.example:5002");
    expect(screen.getByLabelText(/Player Name/i)).toHaveValue("Spacer");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("remembered address prefills the server field", async () => {
    mocked(api.getLastServerAddress).mockResolvedValue("srv.example:5002");
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    await waitFor(() => expect(screen.getByLabelText(/Server Address/i)).toHaveValue("srv.example:5002"));
  });

  it("late remembered address does not overwrite typed input", async () => {
    let resolve: (v: string) => void = () => {};
    mocked(api.getLastServerAddress).mockImplementation(() => new Promise<string>((r) => { resolve = r; }));
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    fireEvent.change(screen.getByLabelText(/Server Address/i), { target: { value: "typed:1" } });
    resolve("srv.example:5002");
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByLabelText(/Server Address/i)).toHaveValue("typed:1");
  });

  it("empty or failed remembered address keeps localhost:5002", async () => {
    mocked(api.getLastServerAddress).mockResolvedValueOnce("");
    const first = render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByLabelText(/Server Address/i)).toHaveValue("localhost:5002");
    first.unmount();

    mocked(api.getLastServerAddress).mockRejectedValueOnce(new Error("boom"));
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByLabelText(/Server Address/i)).toHaveValue("localhost:5002");
  });

  it("session notice lands on the manual stage", async () => {
    useSession.setState({ sessionNotice: "You were removed from the server. Log in again." });
    render(<Welcome />);
    expect(screen.getByLabelText(/Server Address/i)).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("You were removed from the server. Log in again.");
    fireEvent.click(screen.getByText(/^BACK$/));
    expect(useSession.getState().sessionNotice).toBeNull();
  });

  it("CONNECT clears the session notice", async () => {
    useSession.setState({ sessionNotice: "You are banned from this server." });
    render(<Welcome />);
    fireEvent.change(screen.getByLabelText(/FFID/i), { target: { value: "VG12" } });
    fireEvent.click(screen.getByText(/^CONNECT$/i));
    await waitFor(() => expect(api.connect).toHaveBeenCalled());
    expect(useSession.getState().sessionNotice).toBeNull();
  });

  it("FFID label and placeholder", () => {
    render(<Welcome />);
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    expect(screen.getByLabelText("FFID")).toHaveAttribute("placeholder", "e.g. VG12");
  });

  it("welcome rules pin", () => {
    render(<Welcome />);
    expect(screen.getByText(/LOGIN · ORG MEMBER \(SSO\)/).closest("button")).toBeDisabled();
    fireEvent.click(screen.getByText(/JOIN AS GUEST/i));
    const form = screen.getByLabelText(/Server Address/i).closest("form");
    expect(form).not.toBeNull();
    expect(form).toHaveClass("col", "gap-5");
    expect(form!.className).toBe("col gap-5");
  });
});
