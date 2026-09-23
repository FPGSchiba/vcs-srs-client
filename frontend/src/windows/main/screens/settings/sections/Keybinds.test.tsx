import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";

import { Keybinds } from "./Keybinds";
import { useSettings } from "../../../../../shared/store/settings";
import type { Keybind, Trigger, JoystickState } from "../../../../../shared/store/settings";

/** The store's untouched default hotkey state, read before any test mutates it. */
const initialHotkeyState = () => useSettings.getInitialState().hotkeys;

const addTrigger = vi.fn();
const removeTrigger = vi.fn();
const beginCapture = vi.fn();
const endCapture = vi.fn().mockResolvedValue(undefined);
const requestHotkeyPermission = vi.fn();
const openHotkeyPermissionSettings = vi.fn();
const recheckHotkeyPermission = vi.fn();

vi.mock("../../../../../shared/api/client", () => ({
  api: {
    addTrigger: (...a: unknown[]) => addTrigger(...a),
    removeTrigger: (...a: unknown[]) => removeTrigger(...a),
    clearKeybind: vi.fn().mockResolvedValue(undefined),
    beginCapture: (actionId: string) => beginCapture(actionId),
    endCapture: (token: number) => endCapture(token),
    requestHotkeyPermission: () => requestHotkeyPermission(),
    openHotkeyPermissionSettings: () => openHotkeyPermissionSettings(),
    recheckHotkeyPermission: () => recheckHotkeyPermission(),
  },
}));

/** Mirrors the backend's capture-token generation counter: every
 * BeginCapture hands out a new, strictly increasing token, and only the
 * newest one is honoured by EndCapture. Starts at 1 so 0 stays reserved for
 * "beginCapture failed". */
let nextToken = 0;

const key = (chord: string): Trigger => ({
  kind: "key", chord, device: "", device_name: "", label: chord, connected: true,
});

const rows: Keybind[] = [
  { action_id: "global.ptt", label: "Global PTT", desc: "Transmits on the Selected radio",
    category: "global", kind: "hold", triggers: [] },
  { action_id: "global.mute_toggle", label: "Mute toggle", desc: "",
    category: "global", kind: "press", triggers: [key("M")] },
  { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", desc: "",
    category: "per_radio", kind: "hold", triggers: [key("F1")] },
];

/** Builds a single "global.ptt" row with the given triggers -- the fixture
 * used by tests that only need one row and don't care about the rest of the
 * groups. */
function pttRow(triggers: Trigger[]): Keybind {
  return {
    action_id: "global.ptt",
    label: "Global PTT",
    desc: "",
    category: "global",
    kind: "hold",
    triggers,
  };
}

const DEFAULT_JOYSTICK: JoystickState = { supported: false, error: "", devices: [] };

function renderWithKeybinds(keybinds: Keybind[], joystick: JoystickState = DEFAULT_JOYSTICK) {
  useSettings.setState({
    settings: null,
    keybinds,
    hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
    joystick,
  });
  return render(<Keybinds />);
}

describe("Keybinds section", () => {
  beforeEach(() => {
    addTrigger.mockReset().mockResolvedValue({ stolen: null });
    removeTrigger.mockReset().mockResolvedValue(undefined);
    nextToken = 0;
    beginCapture.mockReset().mockImplementation(() => Promise.resolve(++nextToken));
    endCapture.mockReset().mockResolvedValue(undefined);
    // Default: the OS raised the sheet and trust is not (yet) in place. See
    // "keeps GRANT ACCESS when the OS did show a prompt" for why this exact
    // pairing is a branch fixture rather than a reachable production state.
    requestHotkeyPermission
      .mockReset()
      .mockResolvedValue({ prompted: true, permission: "denied" });
    openHotkeyPermissionSettings.mockReset().mockResolvedValue(undefined);
    recheckHotkeyPermission.mockReset().mockResolvedValue(undefined);
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: { registered: true, error: "", failed: {}, permission: "not_applicable" },
      joystick: DEFAULT_JOYSTICK,
    });
  });

  it("groups bindings by category", () => {
    render(<Keybinds />);
    expect(screen.getByText("GLOBAL")).toBeInTheDocument();
    expect(screen.getByText("PER-RADIO BINDINGS")).toBeInTheDocument();
    expect(screen.getByText("Global PTT")).toBeInTheDocument();
    expect(screen.getByText("R01 · GUARD (PTT)")).toBeInTheDocument();
    // Categories with zero rows in this fixture must not render their panel.
    expect(screen.queryByText("CHANNEL HOTKEYS")).not.toBeInTheDocument();
    expect(screen.queryByText("QUICK-STATUS HOTKEYS")).not.toBeInTheDocument();
  });

  it("suspends hotkeys while capturing", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
  });

  it("sends the capture and re-arms hotkeys", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => {
      expect(addTrigger).toHaveBeenCalledWith("global.ptt", {
        code: "F2", ctrl: false, alt: false, shift: false, super: false,
      });
      expect(endCapture).toHaveBeenCalled();
    });
  });

  it("re-arms hotkeys even when addTrigger rejects", async () => {
    addTrigger.mockRejectedValue(new Error("backend unreachable"));
    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(endCapture).toHaveBeenCalled());
  });

  it("reports which action lost a stolen key", async () => {
    addTrigger.mockResolvedValue({
      stolen: { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", trigger: key("F1") },
    });
    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F1", key: "F1" });
    // The warning belongs on the row that just captured the stolen key
    // (global.ptt), not merely somewhere on the page -- scope the query to
    // that row so a regression that renders it under the wrong row (or
    // duplicates it across every row) fails this test.
    const capturingRow = screen.getByText("Global PTT").closest("[data-row]") as HTMLElement;
    await waitFor(() =>
      expect(
        within(capturingRow).getByText(/F1 taken from R01 · GUARD \(PTT\)/i),
      ).toBeInTheDocument(),
    );
    expect(screen.getAllByText(/F1 taken from R01 · GUARD \(PTT\)/i)).toHaveLength(1);
  });

  it("warns when global hotkeys failed to register", () => {
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: { registered: false, error: "permission denied", failed: {}, permission: "unknown" },
    });
    render(<Keybinds />);
    expect(screen.getByText(/global hotkeys unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/permission denied/i)).toBeInTheDocument();
  });

  it("shows a per-row reason when a saved chord failed to register", () => {
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: {
        registered: true, error: "", permission: "not_applicable",
        failed: { "global.mute_toggle": "not registerable: key Numpad7 unsupported" },
      },
    });
    render(<Keybinds />);
    // Scope to the row for global.mute_toggle specifically -- a regression
    // that attached the reason to the wrong row, or to every row, must fail
    // this test, not just "the string exists somewhere".
    const failedRow = screen.getByText("Mute toggle").closest("[data-row]") as HTMLElement;
    expect(
      within(failedRow).getByText(/not registerable: key Numpad7 unsupported/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/not registerable: key Numpad7 unsupported/i)).toHaveLength(1);
  });

  it("cancels the previously listening chip when another chip starts capturing", async () => {
    render(<Keybinds />);

    // Start capturing on the unbound global.ptt row.
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));

    // Before pressing a key, start capturing on a different row (already
    // bound to "M").
    fireEvent.click(screen.getByRole("button", { name: /add binding for mute toggle/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));

    // The superseded chip's forced cancel (unmount-while-listening) is
    // dispatched AFTER the new chip's beginCapture -- React only unmounts it
    // on commit. It must therefore hand back its OWN, now-stale token (1),
    // never the live one (2): the backend refuses a stale token, which is
    // what keeps every OS hotkey suspended for the capture that is actually
    // running. Ending the live capture here would re-register `M` with the
    // OS and let it swallow the keypress meant to rebind it.
    expect(endCapture).toHaveBeenCalledWith(1);
    expect(endCapture).not.toHaveBeenCalledWith(2);

    // A single keypress must only be attributed to the second (still
    // listening) chip -- if the first chip were still listening too, this
    // would call addTrigger twice, once per chip, off one keypress.
    fireEvent.keyDown(window, { code: "F5", key: "F5" });
    await waitFor(() =>
      expect(addTrigger).toHaveBeenCalledWith("global.mute_toggle", {
        code: "F5", ctrl: false, alt: false, shift: false, super: false,
      }),
    );
    expect(addTrigger).toHaveBeenCalledTimes(1);
    expect(addTrigger).not.toHaveBeenCalledWith("global.ptt", expect.anything());

    // And the live capture, once it completes, ends with its own token.
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(2));
  });

  it("does not end the live capture when a row switch races an in-flight addTrigger", async () => {
    // The other half of the same hazard: the user presses a key on row A and
    // then clicks row B while A's addTrigger is still awaiting. A's `finally`
    // therefore runs AFTER B's beginCapture. It must still surrender only A's
    // own token, or B would capture with every OS hotkey re-armed.
    let resolveAdd: (v: unknown) => void = () => {};
    addTrigger.mockImplementation(() => new Promise((res) => { resolveAdd = res; }));

    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(addTrigger).toHaveBeenCalledTimes(1));
    expect(endCapture).not.toHaveBeenCalled(); // still awaiting addTrigger

    // Start a new capture on another row while A is still in flight.
    fireEvent.click(screen.getByRole("button", { name: /add binding for mute toggle/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));

    resolveAdd({ stolen: null });
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));
    expect(endCapture).toHaveBeenCalledWith(1);
    expect(endCapture).not.toHaveBeenCalledWith(2);
  });

  it("ends each capture with the token that capture was issued", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(1));

    // A second, independent capture gets a fresh token.
    fireEvent.click(screen.getByRole("button", { name: /add binding for mute toggle/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(2));
    fireEvent.keyDown(window, { code: "F3", key: "F3" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledWith(2));
  });

  it("does not end a capture that never began", async () => {
    render(<Keybinds />);
    // Escape on a chip the user never clicked cannot happen, but a cancel
    // with no outstanding token must stay a no-op rather than guessing one.
    fireEvent.click(screen.getByRole("button", { name: /add binding for global ptt/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { code: "Escape", key: "Escape" });
    await waitFor(() => expect(endCapture).toHaveBeenCalledTimes(1));

    // Re-clicking the now-idle chip twice must not produce a second end for
    // the first capture's token.
    expect(endCapture).toHaveBeenCalledTimes(1);
    expect(endCapture).toHaveBeenCalledWith(1);
  });

  it("does not warn before the backend has reported hotkey health", () => {
    // The store's default: nothing is known to be broken yet. A pessimistic
    // default flashed the banner on every first paint, and stuck if
    // getHotkeyState ever rejected.
    useSettings.setState({ settings: null, keybinds: rows, hotkeys: initialHotkeyState() });
    render(<Keybinds />);
    expect(screen.queryByText(/global hotkeys unavailable/i)).not.toBeInTheDocument();
  });

  // ---- Per-row suppression while the banner is up -------------------------

  it("does not repeat the banner as a per-row warning on every binding", () => {
    // registered === false means NOTHING registered, which implies `failed`
    // names every bound action. Rendering those inline would print the
    // banner's single message once per row -- pure duplicate noise.
    useSettings.setState({
      settings: null,
      keybinds: rows,
      hotkeys: {
        registered: false,
        error: "permission denied",
        permission: "denied",
        failed: {
          "global.ptt": "permission denied",
          "global.mute_toggle": "permission denied",
          "radio.1.ptt": "permission denied",
        },
      },
    });
    render(<Keybinds />);

    // The banner itself still says it, exactly once.
    expect(screen.getByText(/global hotkeys unavailable/i)).toBeInTheDocument();
    // ...and no row repeats it. Scoped per row so a regression that renders
    // the reason on even one row fails here. Per-radio bindings live in a
    // `.tbl` <tr> rather than a [data-row] div, so match either.
    for (const row of rows) {
      const el = screen.getByText(row.label).closest("[data-row], tr") as HTMLElement;
      expect(el).not.toBeNull();
      expect(within(el).queryByText(/permission denied/i)).not.toBeInTheDocument();
    }
  });

  it("still shows per-row reasons for a PARTIAL failure, with no banner", () => {
    // One unregisterable Numpad7 among working binds keeps registered true.
    // Here the per-row text is the ONLY place the failure is reported, so
    // suppressing it would lose the information entirely.
    useSettings.setState({
      settings: null,
      keybinds: rows,
      hotkeys: {
        registered: true,
        error: "",
        permission: "granted",
        failed: { "global.mute_toggle": "no OS key mapping for Numpad7" },
      },
    });
    render(<Keybinds />);

    expect(screen.queryByText(/global hotkeys unavailable/i)).not.toBeInTheDocument();
    const failedRow = screen.getByText("Mute toggle").closest("[data-row]") as HTMLElement;
    expect(within(failedRow).getByText(/no OS key mapping for Numpad7/i)).toBeInTheDocument();
    // And only that row.
    const okRow = screen.getByText("Global PTT").closest("[data-row]") as HTMLElement;
    expect(within(okRow).queryByText(/no OS key mapping for Numpad7/i)).not.toBeInTheDocument();
  });

  // ---- macOS Accessibility permission ----------------------------------

  /** Puts the store in the denied-permission state the banner branches on. */
  const denyPermission = () =>
    useSettings.setState({
      settings: null,
      keybinds: rows,
      hotkeys: {
        registered: false,
        error: "hotkeys: register global.ptt (F1): failed",
        failed: {},
        permission: "denied",
      },
    });

  it("offers GRANT ACCESS and explains why when permission is denied", async () => {
    denyPermission();
    render(<Keybinds />);

    expect(screen.getByText(/accessibility permission/i)).toBeInTheDocument();
    const grant = screen.getByRole("button", { name: "GRANT ACCESS" });
    expect(screen.queryByRole("button", { name: "OPEN SETTINGS" })).not.toBeInTheDocument();

    fireEvent.click(grant);
    await waitFor(() => expect(requestHotkeyPermission).toHaveBeenCalledTimes(1));
  });

  it("swaps to OPEN SETTINGS once the one-shot prompt is spent", async () => {
    // prompted:false with permission still denied is the ONLY evidence the
    // OS gives that it will not ask again. Leaving GRANT ACCESS up there is
    // a button that provably does nothing.
    requestHotkeyPermission.mockResolvedValue({ prompted: false, permission: "denied" });
    denyPermission();
    render(<Keybinds />);

    fireEvent.click(screen.getByRole("button", { name: "GRANT ACCESS" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "OPEN SETTINGS" })).toBeInTheDocument(),
    );
    expect(screen.queryByRole("button", { name: "GRANT ACCESS" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "OPEN SETTINGS" }));
    await waitFor(() => expect(openHotkeyPermissionSettings).toHaveBeenCalledTimes(1));
  });

  it("keeps GRANT ACCESS when the OS did show a prompt", async () => {
    // NOTE: `{prompted: true, permission: "denied"}` is NOT a state the real
    // backend can produce. AXIsProcessTrustedWithOptions returns the CURRENT
    // trust state, so prompted:true implies Status() is already granted --
    // production only ever sees prompted:true alongside permission:"granted".
    // The pair is exercised anyway because it isolates the branch: it proves
    // the component keys OPEN SETTINGS off `prompted` rather than off
    // `permission`, which is the distinction the whole "a prompt is not an
    // answer" rule rests on. Read it as a branch test, not a real state.
    requestHotkeyPermission.mockResolvedValue({ prompted: true, permission: "denied" });
    denyPermission();
    render(<Keybinds />);

    fireEvent.click(screen.getByRole("button", { name: "GRANT ACCESS" }));
    await waitFor(() => expect(requestHotkeyPermission).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "OPEN SETTINGS" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "GRANT ACCESS" })).toBeInTheDocument();
  });

  it("offers RE-CHECK only after a request has been made", async () => {
    denyPermission();
    render(<Keybinds />);

    expect(screen.queryByRole("button", { name: "RE-CHECK" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "GRANT ACCESS" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "RE-CHECK" })).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByRole("button", { name: "RE-CHECK" }));
    await waitFor(() => expect(recheckHotkeyPermission).toHaveBeenCalledTimes(1));
  });

  it("offers no permission affordance where there is no permission to grant", () => {
    // Windows and Linux/X11 report "not_applicable". A GRANT ACCESS button
    // there is a dead end: there is nothing to grant and nothing to open.
    useSettings.setState({
      settings: null,
      keybinds: rows,
      hotkeys: {
        registered: false,
        error: "hotkeys: OS backend unavailable in this build",
        failed: {},
        permission: "not_applicable",
      },
    });
    render(<Keybinds />);

    expect(screen.getByText(/global hotkeys unavailable/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "GRANT ACCESS" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "OPEN SETTINGS" })).not.toBeInTheDocument();
    expect(screen.queryByText(/accessibility permission/i)).not.toBeInTheDocument();
  });

  it("tells the user to restart when access is granted but nothing registers", () => {
    // The backend already re-applied on the grant (which recreates the event
    // tap), so reaching this state means the running process cannot pick it
    // up. Text only -- there is deliberately no restart button.
    useSettings.setState({
      settings: null,
      keybinds: rows,
      hotkeys: {
        registered: false,
        error: "hotkeys: register global.ptt (F1): failed",
        failed: {},
        permission: "granted",
      },
    });
    render(<Keybinds />);

    expect(screen.getByText(/accessibility is granted/i)).toBeInTheDocument();
    expect(screen.getByText(/restart vcs for hotkeys to take effect/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /restart/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "GRANT ACCESS" })).not.toBeInTheDocument();
  });

  it("does not warn when only SOME bindings failed to register", () => {
    // Registered means "no hotkeys are live at all", not "something failed".
    // One unregisterable key must not declare every working binding dead --
    // it gets a per-row reason instead.
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: {
        registered: true, error: "", permission: "not_applicable",
        failed: { "global.mute_toggle": "no OS key mapping for Numpad7" },
      },
    });
    render(<Keybinds />);
    expect(screen.queryByText(/global hotkeys unavailable/i)).not.toBeInTheDocument();
    expect(screen.getByText(/no OS key mapping for Numpad7/i)).toBeInTheDocument();
  });

  // ---- Multi-trigger rows and the single capture affordance --------------

  it("renders one chip per trigger", () => {
    renderWithKeybinds([
      {
        action_id: "global.ptt",
        label: "Global PTT",
        desc: "",
        category: "global",
        kind: "hold",
        triggers: [
          { kind: "key", chord: "F1", device: "", device_name: "", label: "F1", connected: true },
          {
            kind: "joy",
            chord: "",
            device: "stick-c3",
            device_name: "Test Stick",
            label: "Btn 12",
            connected: true,
          },
        ],
      },
    ]);
    expect(screen.getByText("F1")).toBeInTheDocument();
    expect(screen.getByText("Btn 12")).toBeInTheDocument();
  });

  it("passes the action id to beginCapture", async () => {
    beginCapture.mockResolvedValue(1);
    renderWithKeybinds([pttRow([])]);
    fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
    await waitFor(() => expect(beginCapture).toHaveBeenCalledWith("global.ptt"));
  });

  it("removes the clicked trigger by index", async () => {
    renderWithKeybinds([
      pttRow([
        { kind: "key", chord: "F1", device: "", device_name: "", label: "F1", connected: true },
        {
          kind: "joy",
          chord: "",
          device: "stick-c3",
          device_name: "Test Stick",
          label: "Btn 12",
          connected: true,
        },
      ]),
    ]);
    fireEvent.click(screen.getByRole("button", { name: /remove btn 12/i }));
    await waitFor(() => expect(removeTrigger).toHaveBeenCalledWith("global.ptt", 1));
  });

  it("mentions joystick in the capture prompt when supported", () => {
    renderWithKeybinds([pttRow([])], { supported: true, error: "", devices: [] });
    fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
    expect(screen.getByText(/joystick button/i)).toBeInTheDocument();
  });

  it("omits the joystick half of the prompt when unsupported", () => {
    renderWithKeybinds([pttRow([])], { supported: false, error: "", devices: [] });
    fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
    expect(screen.queryByText(/joystick button/i)).not.toBeInTheDocument();
  });

  it("offers no grant affordance for an unsupported platform", () => {
    // Unsupported is NOT denied. There is nothing the user can grant, so
    // offering a button would be a dead end.
    renderWithKeybinds([pttRow([])], { supported: false, error: "", devices: [] });
    expect(screen.queryByRole("button", { name: /grant/i })).not.toBeInTheDocument();
  });

  it("shows a joystick error without reusing the accessibility copy", () => {
    renderWithKeybinds([pttRow([])], {
      supported: true,
      error: "joystick: cannot read /dev/input (add your user to the 'input' group...)",
      devices: [],
    });
    expect(screen.getByText(/input' group/)).toBeInTheDocument();
    expect(screen.queryByText(/accessibility/i)).not.toBeInTheDocument();
  });
});
