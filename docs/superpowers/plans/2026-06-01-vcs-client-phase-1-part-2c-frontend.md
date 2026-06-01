# VCS Client — Phase 1 Part 2C (Pixel-Faithful Frontend + Round-Trips) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pixel-faithful frontend — Welcome (guest), the frameless shell (header/nav/footer), Home, live Player List, and the Comms pop-out with an editable radio that round-trips through the server — wired to the 2A/2B bindings and events.

**Architecture:** Two Vite entries (`main`, `comms`) share `src/shared`. The design's CSS classes are ported verbatim and reused; React components are ported from the design JSX with their hooks replaced by Zustand stores fed by Wails events. `api/client.ts` wraps generated bindings (mockable in tests).

**Tech Stack:** React 19, TypeScript 6, Vite 8, Tailwind 4, Vitest 4 + Testing Library, Zustand 5, TanStack Router/Query, `@wailsio/runtime`.

**Spec:** [`docs/superpowers/specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md`](../specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md) §4, §6, §9.

**Depends on:** Plans 2A + 2B merged. Bindings available after `wails3 generate bindings -ts` (generated into `frontend/bindings/`, gitignored). Design source to port from: `design/vcs/project/{styles.css, lib/shell.jsx, lib/atoms.jsx, lib/radio.jsx, screens/welcome.jsx, screens/home.jsx}`.

**Environment notes:** `npm run test` (Vitest) and `npm run build` are the gates. Tests mock `src/shared/api/client.ts` — never call the real Wails runtime in unit tests. Event names MUST match the Go `events` constants exactly (`state:client_update`, `state:radio_update`, `auth:session_changed`, `control:connection`, `state:client_left`).

---

## File Structure (after 2C)

```
frontend/src/
  main.tsx                       → renders <MainApp/>
  comms.tsx                      → renders <CommsApp/>
  shared/
    styles/components.css        ported design component classes (imported by global.css)
    api/client.ts                typed wrappers over generated bindings
    api/events.ts                typed event subscribe helpers + event-name constants
    store/session.ts             Zustand: {phase, error, server, self, ping, conn}
    store/clients.ts             Zustand: Record<guid, ClientInfo>
    store/radios.ts              Zustand: Record<guid, RadioInfo>
    components/
      Icon.tsx StatusPill.tsx Button.tsx Field.tsx Toggle.tsx Pill.tsx LcdFreq.tsx
      PreLoginTopBar.tsx TopBar.tsx NavRail.tsx StatusBar.tsx ConnBanner.tsx
  windows/
    main/MainApp.tsx router.tsx
    main/screens/{Welcome.tsx, Home.tsx, Players.tsx, Placeholder.tsx}
    main/screens/Welcome.test.tsx Players.test.tsx
    comms/CommsApp.tsx RadioCard.tsx RadioCard.test.tsx
```

---

## Task 1: Port design CSS

**Files:** Create `frontend/src/shared/styles/components.css`; modify `frontend/src/shared/styles/global.css`.

- [ ] **Step 1:** Copy the **component class rules** (everything after the `:root` token block) from `design/vcs/project/styles.css` into `frontend/src/shared/styles/components.css` — i.e. all the `.topbar`, `.brand`, `.nav`, `.statusbar`, `.panel`, `.tac`, `.btn`, `.input`, `.field`, `.toggle`, `.pill`, `.conn-pill`, `.dual-pill`, `.conn-banner`, `.launcher`, `.user-menu`, `.lcd-screen`, `.lcd-digit`, `.ptt`, `.vu`, `.kbd`, `.mini`, `.launcher-tile`, `.modal`, `.toast`, `.welcome`, `.popout`, `.popout-chrome`, scrollbar, table, and utility (`.row/.col/.gap-*/.flex/.between/.center`) rules. **Do not** copy the `:root{}` block (already in `tokens.css`) nor the `.viewport`/`.desktop` scale-stage rules (dropped — real OS windows). Keep the `.client`/`.popout` panel rules (border-radius/border/shadow) since they now render the window frame.

- [ ] **Step 2:** In `global.css`, add the import after the tokens import:
```css
@import "tailwindcss";
@import "./tokens.css";
@import "./components.css";
@config "../../../tailwind.config.ts";
```
(keep the existing `html, body, #root` block and `*{box-sizing}`)

- [ ] **Step 3:** Build to confirm CSS is valid.

Run: `cd frontend; npm run build`
Expected: build succeeds; `dist/main.html` + `dist/comms.html` emitted.

- [ ] **Step 4: Commit**
```powershell
git add frontend/src/shared/styles
git commit -m "feat(fe): port design component CSS"
```

---

## Task 2: API client + event helpers

**Files:** Create `frontend/src/shared/api/client.ts`, `frontend/src/shared/api/events.ts`.

- [ ] **Step 1:** Generate bindings so the import path exists.

Run: `wails3 generate bindings -ts -clean=true` (from repo root)
Expected: `frontend/bindings/...` regenerated with the new `App` methods (Connect, Disconnect, Reconnect, GetClientState, UpdateRadioInfo, OpenWindow, CloseWindow, GetWindowGeometry, SetWindowGeometry).

- [ ] **Step 2: Write `frontend/src/shared/api/events.ts`**
```ts
import { Events } from "@wailsio/runtime";

// Event names — MUST match internal/events/events.go constants.
export const EV = {
  clientState: "state:client_state",
  clientUpdate: "state:client_update",
  clientLeft: "state:client_left",
  radioUpdate: "state:radio_update",
  settingsUpdate: "state:settings_update",
  serverAction: "state:server_action",
  authSession: "auth:session_changed",
  controlConnection: "control:connection",
} as const;

// on subscribes to a Wails event and returns an unsubscribe function.
export function on<T = unknown>(name: string, cb: (data: T) => void): () => void {
  const off = Events.On(name, (e: { data: T }) => cb(e.data));
  return off;
}
```

- [ ] **Step 3: Write `frontend/src/shared/api/client.ts`** — thin wrappers so components/tests depend on this module, not generated code directly:
```ts
import { App } from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app";

export interface RadioDTO {
  id: number; name: string; frequency: number; enabled: boolean; is_intercom: boolean;
}
export interface RadioInfoDTO { radios: RadioDTO[]; muted: boolean }
export interface ClientInfoDTO { name: string; coalition: string; unit_id: string; role_id: number }
export interface ClientStateSnapshot {
  clients: Record<string, ClientInfoDTO>; radios: Record<string, RadioInfoDTO>;
}

export const api = {
  connect: (server: string, name: string, password: string, unitId: string) =>
    App.Connect(server, name, password, unitId),
  disconnect: () => App.Disconnect(),
  reconnect: () => App.Reconnect(),
  getClientState: (): Promise<ClientStateSnapshot> => App.GetClientState(),
  updateRadioInfo: (info: RadioInfoDTO) => App.UpdateRadioInfo(info),
  openWindow: (id: string) => App.OpenWindow(id),
  closeWindow: (id: string) => App.CloseWindow(id),
};
```
> **Implementer note:** confirm the generated import path/casing for `App` (Wails derives it from the Go package path). Adjust the import line if the generator emits a different module path; the rest of the file is stable. If method names are PascalCase on the generated object (they will be), keep the wrapper lowercase names as the app-facing API.

- [ ] **Step 4:** Build.
Run: `cd frontend; npm run build`
Expected: green.

- [ ] **Step 5: Commit**
```powershell
git add frontend/src/shared/api
git commit -m "feat(fe): typed api client + event helpers over wails bindings"
```

---

## Task 3: Zustand stores

**Files:** Create `frontend/src/shared/store/{session,clients,radios}.ts`.

- [ ] **Step 1: Write `session.ts`**
```ts
import { create } from "zustand";

export type Phase = "welcome" | "connecting" | "connected";
export type Conn = "connected" | "reconnecting" | "disconnected";

interface SessionState {
  phase: Phase;
  conn: Conn;
  error: string | null;
  server: string;
  setPhase: (p: Phase) => void;
  setConn: (c: Conn) => void;
  setError: (e: string | null) => void;
  setServer: (s: string) => void;
}

export const useSession = create<SessionState>((set) => ({
  phase: "welcome",
  conn: "disconnected",
  error: null,
  server: "",
  setPhase: (phase) => set({ phase }),
  setConn: (conn) => set({ conn }),
  setError: (error) => set({ error }),
  setServer: (server) => set({ server }),
}));
```

- [ ] **Step 2: Write `clients.ts`**
```ts
import { create } from "zustand";
import type { ClientInfoDTO } from "../api/client";

interface ClientsState {
  clients: Record<string, ClientInfoDTO>;
  upsert: (guid: string, info: ClientInfoDTO) => void;
  remove: (guid: string) => void;
  replaceAll: (all: Record<string, ClientInfoDTO>) => void;
}

export const useClients = create<ClientsState>((set) => ({
  clients: {},
  upsert: (guid, info) => set((s) => ({ clients: { ...s.clients, [guid]: info } })),
  remove: (guid) => set((s) => {
    const next = { ...s.clients };
    delete next[guid];
    return { clients: next };
  }),
  replaceAll: (clients) => set({ clients }),
}));
```

- [ ] **Step 3: Write `radios.ts`**
```ts
import { create } from "zustand";
import type { RadioInfoDTO } from "../api/client";

interface RadiosState {
  radios: Record<string, RadioInfoDTO>;
  setForGuid: (guid: string, info: RadioInfoDTO) => void;
  replaceAll: (all: Record<string, RadioInfoDTO>) => void;
}

export const useRadios = create<RadiosState>((set) => ({
  radios: {},
  setForGuid: (guid, info) => set((s) => ({ radios: { ...s.radios, [guid]: info } })),
  replaceAll: (radios) => set({ radios }),
}));
```

- [ ] **Step 4:** Build. Run: `cd frontend; npm run build` → green.
- [ ] **Step 5: Commit**
```powershell
git add frontend/src/shared/store
git commit -m "feat(fe): zustand stores for session/clients/radios"
```

---

## Task 4: Port atoms

**Files:** Create `frontend/src/shared/components/{Icon,StatusPill,Button,Field,Toggle,Pill,LcdFreq}.tsx`.

- [ ] **Step 1:** Port from `design/vcs/project/lib/atoms.jsx` (and the LCD/freq bits from `lib/radio.jsx`) into typed components, preserving classNames exactly:
  - `Icon` — port the SVG icon map; props `{ name: string; size?: number; className?: string }`. Keep every icon name used by the chrome (`grid, fleet, users, history, layout, server, admin, settings, help, comms, ship, chat, bell, broadcast, chevron, chevronD, minimize, close, maximize, pin, refresh, shield, unlock, sos, info, x`).
  - `StatusPill` — props `{ kind: "available"|"combat"|"discipline"|"afk"; mini?: boolean }`; classNames `pill pill-<kind>`.
  - `Button` — wraps `<button className="btn …">`; variant props map to `btn-primary/btn-ghost/btn-danger/btn-sm/btn-lg/btn-icon`.
  - `Field` — label + child input using `.field/.field-label` (port the design's `Field`).
  - `Toggle` — `.toggle`/`.toggle.on` controlled component `{ on: boolean; onChange: (v:boolean)=>void }`.
  - `Pill` — generic `.pill` wrapper.
  - `LcdFreq` — the LCD frequency display from `radio.jsx` (`.lcd-screen`/`.lcd-digit`), props `{ value: number }`, read-only render for now (editing handled in RadioCard).

- [ ] **Step 2:** Typecheck. Run: `cd frontend; npx tsc --noEmit` → no errors.
- [ ] **Step 3: Commit**
```powershell
git add frontend/src/shared/components
git commit -m "feat(fe): port design atoms (Icon, StatusPill, Button, Field, Toggle, Pill, LcdFreq)"
```

---

## Task 5: Port shell chrome

**Files:** Create `frontend/src/shared/components/{PreLoginTopBar,TopBar,NavRail,StatusBar,ConnBanner}.tsx`.

- [ ] **Step 1:** Port from `design/vcs/project/lib/shell.jsx`, preserving classNames and layout, with these adaptations:
  - **PreLoginTopBar / TopBar**: keep `-webkit-app-region: drag` on `.topbar` (it's in the CSS); window controls (`.win-ctrl` minimize/close) call `@wailsio/runtime` Window methods:
    ```ts
    import { Window } from "@wailsio/runtime";
    // minimize:
    Window.Minimise();
    // close:
    Window.Close();
    ```
  - **TopBar** Comms launcher button calls `api.openWindow("comms")`. The other launchers (fleet/ship/messages/notifications) render but are disabled/no-op this phase (title="coming in a later phase").
  - **NavRail**: port `NAV_ITEMS`; `Home` and `Player List` are active routes (TanStack Router links), the rest navigate to a placeholder route. Drop the design's mock `app.user` data; read callsign/coalition from the session/clients store (fallbacks to "—" when not connected).
  - **StatusBar**: render the **control-only** form — a single status item with the control node + live ping from the session store (no dual control/voice pill this phase). Keep the `.statusbar` layout and the NETWORK/HELP/ALERTS buttons (ALERTS/NETWORK are no-ops/placeholder routes for now).
  - **ConnBanner**: port, but only the `disconnected` variant renders (control-only); its action button calls `api.reconnect()`.

- [ ] **Step 2:** Typecheck. Run: `cd frontend; npx tsc --noEmit` → no errors.
- [ ] **Step 3: Commit**
```powershell
git add frontend/src/shared/components
git commit -m "feat(fe): port shell chrome (TopBar, NavRail, StatusBar, ConnBanner)"
```

---

## Task 6: Welcome screen (guest) + test

**Files:** Create `frontend/src/windows/main/screens/Welcome.tsx`, `Welcome.test.tsx`.

- [ ] **Step 1: Write the failing test** `Welcome.test.tsx`
```tsx
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
```

- [ ] **Step 2: Run to verify failure**
Run: `cd frontend; npm run test`
Expected: FAIL — `Welcome` not found.

- [ ] **Step 3: Write `Welcome.tsx`** — port the design's `welcome.jsx` **welcome + manual(guest) stages** (skip the SSO `select` stage — that's Phase 2). Use labeled inputs (`<Field label=...>` must associate the label with the input via htmlFor/id so `getByLabelText` works). On CONNECT:
```tsx
import { useState } from "react";
import { api } from "../../../shared/api/client";
import { useSession } from "../../../shared/store/session";

export function Welcome() {
  const [stage, setStage] = useState<"welcome" | "manual">("welcome");
  const [server, setServer] = useState("localhost:5002");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [ffid, setFfid] = useState("");
  const [error, setError] = useState<string | null>(null);
  const setPhase = useSession((s) => s.setPhase);

  async function connect() {
    setError(null);
    try {
      setPhase("connecting");
      await api.connect(server, name, password, ffid);
      // success transitions arrive via control:connection events (wired in MainApp)
    } catch (e) {
      setPhase("welcome");
      setError(e instanceof Error ? e.message : "connection failed");
    }
  }
  // ... render the ported welcome/manual markup, wiring inputs to the state above,
  // the GUEST button → setStage("manual"), CONNECT → connect(), and rendering
  // {error && <div className="cap" style={{color:"var(--ac-alert)"}}>{error}</div>}
}
```
Port the exact panel/markup from `welcome.jsx` for visual fidelity; ensure inputs have `id`+`<label htmlFor>` (or `aria-label`) for "Server Address", "Server Password", "Player Name", "FFID (optional)".

- [ ] **Step 4: Run tests** → PASS (2).
- [ ] **Step 5: Commit**
```powershell
git add frontend/src/windows/main/screens/Welcome.tsx frontend/src/windows/main/screens/Welcome.test.tsx
git commit -m "feat(fe): guest Welcome screen with connect + inline errors"
```

---

## Task 7: MainApp, router, Home, Players (+ test)

**Files:** Create `frontend/src/windows/main/{MainApp,router}.tsx`, `screens/{Home,Players,Placeholder}.tsx`, `screens/Players.test.tsx`; modify `frontend/src/main.tsx`.

- [ ] **Step 1: Write the failing test** `Players.test.tsx`
```tsx
import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { useClients } from "../../../shared/store/clients";
import { Players } from "./Players";

describe("Players", () => {
  beforeEach(() => useClients.setState({ clients: {} }));

  it("renders a row per connected client", () => {
    useClients.setState({
      clients: {
        g1: { name: "Alice", coalition: "VG", unit_id: "DSC", role_id: 1 },
        g2: { name: "Bob", coalition: "VG", unit_id: "SHN", role_id: 0 },
      },
    });
    render(<Players />);
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("Bob")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify failure** → FAIL (`Players` not found).

- [ ] **Step 3: Write the screens + router + MainApp:**
  - `Players.tsx`: read `useClients().clients`, render a `.tbl` table (port the player-list styling) with a row per entry (name, coalition, unit, role).
  - `Home.tsx`: port `screens/home.jsx` launcher tiles (the Comms tile click → `api.openWindow("comms")`); other tiles render but are inert this phase.
  - `Placeholder.tsx`: a `.state-card` reading "Arrives in a later phase".
  - `router.tsx`: TanStack Router with routes `/` (Home), `/players` (Players), and catch-all/other nav keys → Placeholder.
  - `MainApp.tsx`: on mount, `api.getClientState()` → hydrate `useClients`/`useRadios`; subscribe via `on(EV.*)` to update stores and `useSession` (`control:connection` → `setConn`+`setPhase`, `state:client_update`→upsert, `state:client_left`→remove, `state:radio_update`→radios.setForGuid). Render `<Welcome/>` when `phase !== "connected"`, else the shell (`TopBar` + `NavRail` + router outlet + `StatusBar`, with `ConnBanner` under the top bar). Unsubscribe on unmount.
  - `main.tsx`: `ReactDOM.createRoot(...).render(<React.StrictMode><MainApp/></React.StrictMode>)` importing `./shared/styles/global.css`.

- [ ] **Step 4: Run tests** → PASS.
- [ ] **Step 5:** Build. Run: `cd frontend; npm run build` → green, `dist/main.html` emitted.
- [ ] **Step 6: Commit**
```powershell
git add frontend/src/windows/main frontend/src/main.tsx
git commit -m "feat(fe): MainApp shell, router, Home + live Player List"
```

---

## Task 8: Comms window — RadioCard round-trip (+ test)

**Files:** Create `frontend/src/windows/comms/{CommsApp,RadioCard}.tsx`, `RadioCard.test.tsx`; modify `frontend/src/comms.tsx`.

- [ ] **Step 1: Write the failing test** `RadioCard.test.tsx`
```tsx
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
```

- [ ] **Step 2: Run to verify failure** → FAIL (`RadioCard` not found).

- [ ] **Step 3: Write `RadioCard.tsx`** — port the radio-strip visuals from `design/vcs/project/lib/radio.jsx` (LCD freq, name, enable/intercom toggles, PTT visuals are display-only this phase). Edit behavior is **write-through**: hold transient local input, and on commit build the full `RadioInfoDTO` (the edited radio merged into `allRadios`) and call `api.updateRadioInfo`. The store updates when the `state:radio_update` echo arrives (handled in CommsApp), so do not mutate the store optimistically.
```tsx
import { useState } from "react";
import { api, type RadioDTO } from "../../shared/api/client";

interface Props { radio: RadioDTO; allRadios: RadioDTO[]; muted: boolean }

export function RadioCard({ radio, allRadios, muted }: Props) {
  const [name, setName] = useState(radio.name);

  function commit(next: RadioDTO) {
    const radios = allRadios.map((r) => (r.id === next.id ? next : r));
    void api.updateRadioInfo({ muted, radios });
  }
  // render ported markup:
  //  - name <input aria-label="radio name" value={name}
  //      onChange=(set local) onBlur=()=>commit({...radio, name})>
  //  - enabled toggle: <button aria-label="toggle enabled"
  //      onClick=()=>commit({...radio, enabled: !radio.enabled})>
  //  - intercom toggle: aria-label="toggle intercom" → commit({...radio, is_intercom: !radio.is_intercom})
  //  - LcdFreq value={radio.frequency} (freq editing can stay display-only this phase)
}
```

- [ ] **Step 4: Write `CommsApp.tsx`** — on mount, `api.getClientState()` to find the **self** radios (Phase-1 simplification: render the first radios entry, or the self guid once exposed; for now render `Object.values(radios)[0]`), subscribe to `state:radio_update` to refresh. Render a `.popout` body with the `.popout-chrome` header (title "Communications", close button → `Window.Close()`) and a `RadioCard` per radio. `comms.tsx`: render `<CommsApp/>` with `global.css` imported.

- [ ] **Step 5: Run tests** → PASS (2).
- [ ] **Step 6:** Build → green, `dist/comms.html` emitted.
- [ ] **Step 7: Commit**
```powershell
git add frontend/src/windows/comms frontend/src/comms.tsx
git commit -m "feat(fe): Comms popout with write-through radio editing"
```

---

## Task 9: Full verification + DoD smoke

**Files:** none (verification + any small fixes).

- [ ] **Step 1: Frontend gates**
Run: `cd frontend; npm run test; npm run build`
Expected: all Vitest suites pass; both HTML entries emit.

- [ ] **Step 2: Backend + vet (from repo root)**
Run: `go test ./internal/... ./pkg/...; go vet ./...; go build ./...`
Expected: green.

- [ ] **Step 3: Manual smoke (informal, non-gating)** — with a local `vcs-srs-server` running:
Run: `task dev`
Walk the DoD (spec §9): frameless/transparent main window with ported chrome → guest CONNECT → Home → open Comms (second window) → edit a radio → see it echo back; drag the title bar; minimize/close via custom controls; disconnect → back to Welcome. Note anything that needs follow-up.

- [ ] **Step 4: Flip the roadmap** — in `docs/ROADMAP.md`, set Phase 1 status to `[x]` (Part 1 + Part 2 complete) and add a one-line note that Part 2 landed across plans 2A/2B/2C.

- [ ] **Step 5: Commit**
```powershell
git add docs/ROADMAP.md
git commit -m "docs: mark Roadmap Phase 1 complete"
```

---

## Self-Review (performed)

**Spec coverage (§4, §6, §9):** CSS port ✓ (T1); api/events ✓ (T2); stores ✓ (T3); atoms ✓ (T4); chrome incl. control-only StatusBar + disconnected-only ConnBanner ✓ (T5); guest Welcome + inline errors ✓ (T6); MainApp event wiring + Home + live Players ✓ (T7); Comms write-through round-trip ✓ (T8); full gates + DoD smoke + roadmap flip ✓ (T9). Window drag (`-webkit-app-region`) + min/close (`@wailsio/runtime` Window) ✓ (T5). Geometry reporting via `SetWindowGeometry` is available from 2B; wiring move/resize listeners is optional polish — if the Wails window exposes move/resize events to JS, subscribe in MainApp/CommsApp and debounce-call `App.SetWindowGeometry`; otherwise geometry persists on close via the Go registry (2B), which satisfies the DoD.

**Placeholder scan:** "port from `design/...`" instructions reference concrete in-repo files (actionable, not TODOs). All test code and wiring code is complete and runnable. The two "confirm generated import path / Wails Window API" notes are real verification points isolated to `api/client.ts` and the chrome window-control calls.

**Type consistency:** `RadioDTO`/`RadioInfoDTO`/`ClientInfoDTO`/`ClientStateSnapshot` field names (snake_case `is_intercom`, `unit_id`, `role_id`) match the Go DTO json tags from 2A (`app/dto.go`); event-name constants in `events.ts` match `internal/events/events.go`; store method names (`upsert`/`remove`/`replaceAll`/`setForGuid`/`setConn`/`setPhase`) are used consistently across MainApp wiring and tests.
