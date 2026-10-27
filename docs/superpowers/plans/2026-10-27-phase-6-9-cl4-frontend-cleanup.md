# Phase 6.9 CL4 — Frontend Cleanup & Stack Trim Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Execution context:** run this plan in a **fresh git worktree** of the client repo (`D:\Projects\vcs-srs-client`) on branch **`chore/cleanup-cl4`**, branched from **`origin/main`**, not from `docs/phase-6-9-umbrella-spec`:
`git fetch origin && git worktree add -b chore/cleanup-cl4 ../vcs-srs-client-cl4 origin/main`. All paths below are relative to that worktree. Before Task 1, run `cd frontend && npm ci`, then generate bindings with `wails3 generate bindings -ts -f '-tags purego'` from the repo root. The bindings are gitignored.

**Goal:** Remove dead frontend code, unused deps and Tailwind (rendering-neutral except for one deliberate pulse change). Fix the Settings stubs. Make the binding wrapper fail at compile time on Go/TS drift.

**Architecture:** Purely frontend. Every task is TDD against vitest and/or `tsc`. Tailwind removal ports the relied-on preflight subset into `global.css` `@layer base`. Neutrality is proved by a computed-style diff of origin/main vs the branch, using an uncommitted capture harness.

**Tech Stack:** React 19, TypeScript 7, Vite 8, Vitest 5, Zustand, Wails v3 generated bindings.

**Spec:** `docs/superpowers/specs/2026-10-27-phase-6-9-cl4-frontend-cleanup-design.md` (committed by Task 8; until then it is at the scratchpad path `design-CL4.md` handed to you). The umbrella is `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md`, §3 CL4 and §9.

## Global Constraints

- **No new dependencies.** Only the 7 removals listed in Tasks 1 and 5.
- Go code is untouched. The Go suite runs once in Task 8.
- No change to classNames, the disabled SSO button or the layout of `Welcome.tsx` beyond removing the `setSessionServer` read and call.
- Rendering must be identical to origin/main except for the allow-list: (a) `display` on the 3 `className="flex …"` elements (`History.tsx:118`, `Profiles.tsx:189`, `NotificationsApp.tsx:117`); (b) Settings sections Profiles, Notifications and Legacy (FE07); (c) `@keyframes pulse` 50% opacity 0.5 → **0.4** (user decision D1).
- Exact copy (user decision D2): stub title **`Not yet scheduled`**; Notifications items `Alert rules` and `Server notifications (needs server support)`; Notifications note `Notification sounds: General → Play notification sounds.`; Profiles text `Profiles and layouts are managed on the Profiles screen.` and button `OPEN PROFILES`.
- Misc stays `phase={10}`, which renders "Arrives in Phase 10".
- Frontend gate commands (run in `frontend/`): `npx vitest run`, `npx tsc --noEmit`, `npm run build`.
- Client Go gate (repo root): `go build -tags purego ./...`, `go vet -tags purego ./...`, `go test -race -count=1 -tags purego ./...`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Placeholder text in inputs** (Welcome, History search, Profiles name). Without preflight's `::placeholder` rule, the text renders in the UA's grey instead of 50% currentcolor. Pinned by `styles.test.ts` (Task 5) and by the computed-style diff `::placeholder` dump (Task 7).
2. **Inline SVG icons shifting baseline.** Without `svg{display:block}`, every `Icon` becomes inline and rows gain descender gaps. Pinned in Task 5's test and checked in Task 7.
3. **The History "Channel" label becoming visible** if `.sr-only` is lost. Pinned in Task 5's test, and Task 7 includes the History screen.
4. **Settings → Profiles button with no `onNavigate`** (SettingsScreen rendered standalone in tests or other shells) must not throw. Pinned by a Task 3 test.
5. **A Go DTO field rename passing silently** through the narrowed calls. Pinned by Task 6's deliberate-drift red step.

---

### Task 1: FE11 — drop unused runtime deps

**GATE — needs user approval before this task:** removes npm deps `@tanstack/react-query@5.104.5`, `@tanstack/react-router@1.170.46` and `zod@4.6.7` (dependency change, stack deviation from the master spec).

**Files:**
- Create: `frontend/src/stack.test.ts`
- Modify: `frontend/package.json`, `frontend/package-lock.json`

**Interfaces:**
- Produces: `stack.test.ts` with a `REMOVED` string array. Task 5 appends to it.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import raw from "../package.json?raw";

const pkg = JSON.parse(raw) as { dependencies?: Record<string, string>; devDependencies?: Record<string, string> };
const REMOVED = ["@tanstack/react-query", "@tanstack/react-router", "zod"];

describe("frontend stack", () => {
  it.each(REMOVED)("does not declare %s", (name) => {
    expect(pkg.dependencies ?? {}).not.toHaveProperty([name]);
    expect(pkg.devDependencies ?? {}).not.toHaveProperty([name]);
  });
});
```

- [ ] **Step 2: Run it and confirm it fails.** `npx vitest run src/stack.test.ts`. Expected: 3 failures. If `?raw` on JSON is a TS error, add `/// <reference types="vite/client" />` at the top. It is already in scope if the RadioFrame test compiles.
- [ ] **Step 3: Uninstall.** `npm uninstall @tanstack/react-query @tanstack/react-router zod`. `git diff package-lock.json` must show only removals.
- [ ] **Step 4: Verify.** `npx vitest run src/stack.test.ts` passes. `npx tsc --noEmit` is clean. `npm run build` succeeds.
- [ ] **Step 5: Commit.** `git add frontend/package.json frontend/package-lock.json frontend/src/stack.test.ts && git commit -m "chore(frontend): drop unused tanstack and zod deps (FE11)"`

---

### Task 2: FE10 + FE19 — dead components, store members, KeyChip chord branch, TopBar comment

**GATE — needs user approval before this task:** deletes tracked files `frontend/src/shared/components/Placeholder.tsx`, `Placeholder.test.tsx`, `Pill.tsx` and `StatusPill.tsx`.

**Files:**
- Create: `frontend/src/shared/deadcode.test.ts`
- Delete: `frontend/src/shared/components/{Placeholder,Placeholder.test,Pill,StatusPill}.tsx`
- Modify: `frontend/src/shared/store/session.ts`, `frontend/src/shared/store/windows.ts`, `frontend/src/windows/main/screens/Welcome.tsx` (lines 59, 77), `frontend/src/windows/main/screens/Welcome.test.tsx:22`, `frontend/src/shared/components/KeyChip.tsx`, `frontend/src/shared/components/KeyChip.test.tsx`, `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx` (the `<KeyChip binding="" …>` call), `frontend/src/shared/components/TopBar.tsx:43-52`
- Do **not** touch `frontend/src/windows/main/screens/Placeholder.tsx`. It is live in `MainApp`.

**Interfaces:**
- Produces: `SessionState` without `error`, `server`, `setError` or `setServer`. `WindowsState` = `{ open: string[]; setOpen(open: string[]): void }`. `KeyChipProps` without `binding`.

- [ ] **Step 1: Write the failing test** `deadcode.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { useSession } from "./store/session";
import { useWindows } from "./store/windows";

const components = Object.keys(import.meta.glob("./components/*.tsx"));

describe("dead code stays deleted", () => {
  it.each(["Placeholder.tsx", "Pill.tsx", "StatusPill.tsx"])("%s is gone", (f) => {
    expect(components).not.toContain(`./components/${f}`);
  });
  it("stores expose no write-only members", () => {
    const s = useSession.getState();
    const w = useWindows.getState();
    // @ts-expect-error removed in CL4 (FE10)
    void s.server;
    // @ts-expect-error removed in CL4 (FE10)
    void s.error;
    // @ts-expect-error removed in CL4 (FE10)
    void w.isOpen;
    expect(Object.keys(s)).not.toEqual(expect.arrayContaining(["server", "error", "setServer", "setError"]));
    expect(Object.keys(w)).not.toContain("isOpen");
  });
});
```

In `KeyChip.test.tsx`, delete the `"renders the bound chord"` test, remove every `binding=…` prop, and in `"cancels on Escape without capturing"` arm the chip by clicking the em dash (`screen.getByText("—")`).

- [ ] **Step 2: Run and confirm red.** `npx vitest run src/shared/deadcode.test.ts` fails on the three file assertions and the key assertions. `npx tsc --noEmit` reports unused `@ts-expect-error` directives and the missing `binding` prop in KeyChip.test.
- [ ] **Step 3: Implement.** Delete the 4 files. Trim `session.ts` and `windows.ts` (drop the `get` parameter). Remove `setSessionServer` from `Welcome.tsx`, and in `Welcome.test.tsx:22` set `{ phase: "welcome", conn: "disconnected" }`. In `KeyChip`, remove the `binding` prop and the chord-render branch, so the non-listening render is the `.kbd.unbound` em dash. Update the `KeyChip` doc comment. Remove `binding=""` from `Keybinds.tsx`. In `TopBar.tsx`, move the doc comment above `export function TopBar` and replace its identity sentence with: "The user menu shows the callsign and initials from `useSession().self`, falling back to `GUEST` / `—` before a snapshot is hydrated."
- [ ] **Step 4: Verify.** `npx vitest run`, `npx tsc --noEmit` and `npm run build` are all green.
- [ ] **Step 5: Commit.** `git add -A frontend/src && git commit -m "refactor(frontend): delete dead components, store members and KeyChip chord branch (FE10, FE19)"`

---

### Task 3: FE07 — Settings stubs stop advertising shipped phases

**GATE — needs user approval before this task:** user-visible copy change in Settings (Profiles, Notifications, Legacy), as approved in umbrella §9 (D2). The approval here is of the exact strings in Global Constraints.

**Files:**
- Modify: `frontend/src/windows/main/screens/settings/sections/Deferred.tsx`, `frontend/src/windows/main/screens/settings/SettingsScreen.tsx`, `frontend/src/windows/main/MainApp.tsx` (`SCREENS`), `frontend/src/windows/main/screens/settings/SettingsScreen.test.tsx`

**Interfaces:**
- Produces: `DeferredProps = { title?: string; phase?: number; items?: string[]; note?: string }`. `SettingsScreen({ onNavigate }: { onNavigate?: (view: string) => void })`. In `MainApp`, `SCREENS` becomes `(nav: (v: string) => void) => Record<string, () => ReactElement>`, or equivalent, with `settings: () => <SettingsScreen onNavigate={nav} />`.

- [ ] **Step 1: Write failing tests** in `SettingsScreen.test.tsx`. Replace the two existing "deferred notice" tests (lines 84-95) with:
  - `"points Profiles & Layouts at the Profiles screen"`: render with `onNavigate = vi.fn()`, click "Profiles & Layouts". Expect the text `Profiles and layouts are managed on the Profiles screen.` Click `OPEN PROFILES`, then `expect(onNavigate).toHaveBeenCalledWith("profiles")`. Expect `queryByText(/Arrives in Phase/i)` to be null.
  - `"Profiles button is inert without onNavigate"`: render with no props, click the section, then click `OPEN PROFILES`. No throw.
  - `"Notifications is not yet scheduled and points sounds at General"`: expect `Not yet scheduled`, `Alert rules`, `Server notifications (needs server support)` and `Notification sounds: General → Play notification sounds.`. `queryByText(/Sound alerts/)` is null, and so is `/Arrives in Phase/`.
  - `"Legacy is not yet scheduled"`: expect `Not yet scheduled` and the 3 legacy items. No `/Arrives in Phase/`.
  - `"only Miscellaneous names a phase"`: for each of the 8 sections except Miscellaneous, click it and assert `queryByText(/Arrives in Phase/i)` is null. On Miscellaneous, assert `Arrives in Phase 10`.
- [ ] **Step 2: Run and confirm red.** `npx vitest run src/windows/main/screens/settings/SettingsScreen.test.tsx`. Expected: the new tests fail because they find "Arrives in Phase 7" or "Arrives in Phase 4" and do not find the new copy.
- [ ] **Step 3: Implement.** In `Deferred`, `phase` is optional. The state-title is `phase !== undefined ? \`Arrives in Phase ${phase}\` : "Not yet scheduled"`, and `note`, when set, renders as a `<div className="cap-dim" style={{ fontSize: 11 }}>` under the title. Rewrite the doc comment to describe both modes and drop the "Task 10 brief" and "Task 11" text. `SettingsScreen` renders the Profiles panel (`Panel title="PROFILES & LAYOUTS"`, a `state-card` holding the text and `<button className="btn" onClick={() => onNavigate?.("profiles")}>OPEN PROFILES</button>`) and the Notifications and Legacy stubs per the spec. Wire `MainApp` to pass `setView`.
- [ ] **Step 4: Verify.** `npx vitest run`, `npx tsc --noEmit` and `npm run build` are green.
- [ ] **Step 5: Commit.** `git commit -am "feat(settings): honest stubs for shipped and unscheduled sections (FE07)"`

---

### Task 4: FE16 — `.popout.native` modifier, delete dead `.client` CSS

**Files:**
- Create: `frontend/src/shared/styles/styles.test.ts`
- Modify: `frontend/vitest.config.ts` (`css.include`), `frontend/src/shared/styles/components.css` (lines 8-24, after `.popout`), `frontend/src/windows/comms/CommsApp.tsx:199-211`, `frontend/src/windows/notifications/NotificationsApp.tsx:75-87`, `CommsApp.test.tsx`, `NotificationsApp.test.tsx`

**Interfaces:**
- Produces: `styles.test.ts` exporting nothing, importing `components` (`components.css?raw`) and `global` (`global.css?raw`). Task 5 adds cases. CSS class `.popout.native`.

- [ ] **Step 1: Widen vitest CSS.** In `vitest.config.ts`, set `css: { include: [/tokens\.css/, /components\.css/, /global\.css/] }` and update the comment. Then confirm that `npx vitest run src/windows/comms/RadioFrame.test.tsx` still passes.
- [ ] **Step 2: Write failing tests.**
  - `styles.test.ts`: `"defines .popout.native"` asserts `components` matches `/\.popout\.native\s*\{[^}]*position:\s*static[^}]*\}/`, with every declaration present: `position:static; width:100%; height:100%; border:none; border-radius:0; box-shadow:none; min-width:0; min-height:0`. `"has no dead .client rule"` asserts that `components` does not match `/(^|\n)\.client[\s.{]/`.
  - `CommsApp.test.tsx` and `NotificationsApp.test.tsx`: new test `"renders the native popout shell without inline style"`. After render, `const root = container.querySelector(".popout")!`. Expect `root.classList.contains("native")` to be true and `root.getAttribute("style")` to be null.
- [ ] **Step 3: Run and confirm red.** `npx vitest run src/shared/styles src/windows/comms/CommsApp.test.tsx src/windows/notifications/NotificationsApp.test.tsx`
- [ ] **Step 4: Implement.** Add the rule right after `.popout { … }`. Delete `.client` and `.client.active` (keep `.topbar` and `.topbar > *`). Both roots become `<div className="popout native">`.
- [ ] **Step 5: Verify.** `npx vitest run`, `npx tsc --noEmit` and `npm run build` are green.
- [ ] **Step 6: Commit.** `git commit -am "refactor(frontend): .popout.native replaces duplicated popout inline style (FE16)"`

---

### Task 5: FE12 — remove Tailwind, port the relied-on preflight subset

**GATE — needs user approval before this task:** (1) removes npm devDeps `tailwindcss@4.3.3`, `@tailwindcss/postcss@4.3.3`, `autoprefixer@10.6.1` and `postcss@8.5.31` (the direct entry; `postcss` stays transitive via vite); (2) deletes tracked files `frontend/tailwind.config.ts` and `frontend/postcss.config.cjs`; (3) user-visible change: the pulse animation dips to opacity 0.4 instead of 0.5 (D1, approved in umbrella §9 and called out in the PR); (4) stack deviation from the master spec, which DOC records.

**Files:**
- Modify: `frontend/src/shared/styles/global.css`, `frontend/src/shared/styles/components.css` (add `.sr-only`), `frontend/src/shared/styles/styles.test.ts`, `frontend/src/stack.test.ts`, `frontend/package.json`, `frontend/package-lock.json`
- Delete: `frontend/tailwind.config.ts`, `frontend/postcss.config.cjs`

- [ ] **Step 0: Capture the baseline CSS before changing anything.** Run `npm run build`, then copy `dist/assets/*.css` to `frontend/.capture/baseline.css` (add `frontend/.capture/` to `.git/info/exclude` now so it is never staged). Every value ported below is copied from this file, not retyped.
- [ ] **Step 1: Write failing tests.**
  - `stack.test.ts`: append `"tailwindcss", "@tailwindcss/postcss", "autoprefixer", "postcss"` to `REMOVED`.
  - `styles.test.ts`, add:
    - `"has no Tailwind directives"`: `global` does not match `/@import\s+["']tailwindcss["']/` or `/@config/`.
    - `"defines .sr-only"`: `components` matches `/\.sr-only\s*\{[^}]*position:\s*absolute[^}]*clip-path:\s*inset\(50%\)[^}]*\}/`.
    - `"ports the relied-on preflight inside @layer base"`: `global` contains `@layer base`, and inside that block (slice from `@layer base` to the end) matches each of: `/::backdrop[^{]*\{[^}]*box-sizing:\s*border-box/`, `/svg[^{]*\{[^}]*display:\s*block/`, `/button,\s*input,\s*select[^{]*\{[^}]*font:\s*inherit/`, `/::placeholder\s*\{[^}]*opacity:\s*1/`, `/table\s*\{[^}]*border-collapse:\s*collapse/`, `/ul,\s*menu\s*\{[^}]*list-style:\s*none|ol,\s*ul,\s*menu\s*\{[^}]*list-style:\s*none/`, `/b,\s*strong\s*\{[^}]*font-weight:\s*bolder/`, `/-webkit-inner-spin-button[^{]*\{[^}]*height:\s*auto/`.
    - `"keeps the design's pulse keyframe (D1)"`: `components.match(/@keyframes pulse/g)` has length 1, and the block matches `/50%\s*\{\s*opacity:\s*0\.4;?\s*\}/`. `global` has no `@keyframes pulse`.
- [ ] **Step 2: Run and confirm red.** `npx vitest run src/stack.test.ts src/shared/styles`. Expected: the 4 package failures, plus the directive, `.sr-only` and `@layer base` failures. The pulse test may already pass on source. It guards against a reintroduced keyframe, and the change it represents shows up in built output (Task 7).
- [ ] **Step 3: Implement.**
  - In `global.css`, delete `@import "tailwindcss";` and the `@config` line **with its whole comment block** (otherwise the comment stays unclosed). Keep the `tokens.css` and `components.css` imports first. Add `@layer base { … }` holding resets 1–11 from spec §4.4, with values copied from `baseline.css`'s `@layer base`. Open it with the comment "Adapted from Tailwind CSS v4 preflight (MIT). Only resets for elements the app renders; dropped: hr, abbr, h1-h6, a, code/kbd/samp/pre, small, sub/sup, progress, summary, textarea resize, img/video max-width, date/time/search/file pseudo-elements, :-moz-*." Leave the existing unlayered `html, body, #root` and `*` rules as they are.
  - Add `.sr-only` to `components.css`, with declarations copied from `baseline.css`'s `.sr-only` utility.
  - Delete `tailwind.config.ts` and `postcss.config.cjs`, then run `npm uninstall tailwindcss @tailwindcss/postcss autoprefixer postcss`.
- [ ] **Step 4: Verify.** `npx vitest run`, `npx tsc --noEmit` and `npm run build` are green. Then run `grep -c "tailwind" dist/assets/*.css`, which must print `0`. In the built CSS, `@keyframes pulse` must be the 0.4 variant.
- [ ] **Step 5: Commit.** `git add -A frontend && git commit -m "chore(frontend): remove Tailwind; port relied-on preflight into @layer base (FE12)"`

---

### Task 6: FE13 — binding wrapper aliases generated types, drift fails `tsc`

**Files:**
- Modify: `frontend/src/shared/api/client.ts`

**Interfaces:**
- Consumes: `import type * as models from "../../../bindings/github.com/FPGSchiba/vcs-srs-client/internal/app/models"`. Generated classes: `RadioDTO`, `RadioInfoDTO`, `ClientInfoDTO`, `ClientStateSnapshot`, `BuildInfoDTO`, `VoiceStateDTO`, `ProfileBlockDTO`, `ProfileWindowDTO`, `LayoutDTO`, `ProfileSummaryDTO`, `ProfileStateDTO`, `HistoryEntryDTO`, `SettingsDTO`, `KeybindDTO`, `TriggerDTO`, `StolenDTO`, `HotkeyStateDTO`, `SetKeybindResult`, …
- Produces: the same exported type names consumers import today (`RadioDTO`, `RadioInfoDTO`, `ClientInfoDTO`, `ClientStateSnapshot`, `BuildInfo`, `VoiceStateDTO`, `LayoutBlock`, `LayoutWindow`, `Layout`, `ProfileSummary`, `ProfileState`, `HistoryEntry`) and the same `api` keys and parameter lists.
  - `type SameKeys<A, B> = [Exclude<keyof A, keyof B>, Exclude<keyof B, keyof A>] extends [never, never] ? true : false;`
  - `function narrow<Fe>(p: Promise<unknown>): Promise<Fe>`. This is the **only** cast left in the file. Its doc comment says it is valid only for a call whose key sets are pinned by a `SameKeys` assertion.
  - The guards are `const _drift: { [K in "trigger" | "stolen" | "notifyItem" | …]: true } = { … }` (or individual `const _x: SameKeys<models.TriggerDTO, Trigger> = true;` lines). Write one per narrowed DTO and per nested DTO it contains.

- [ ] **Step 1: Write the failing check (red via `tsc`).** Replace the 11 hand interfaces with aliases: `export type RadioDTO = models.RadioDTO;`, `BuildInfo = models.BuildInfoDTO`, `LayoutBlock = models.ProfileBlockDTO`, `LayoutWindow = models.ProfileWindowDTO`, `Layout = models.LayoutDTO`, `ProfileSummary = models.ProfileSummaryDTO`, `ProfileState = models.ProfileStateDTO`, `HistoryEntry = models.HistoryEntryDTO`, and so on. Keep `ClientStateSnapshot` as a frontend interface (`clients: Record<string, ClientInfoDTO>; radios: Record<string, RadioInfoDTO>; self_guid: string; self: ClientInfoDTO | null`). Remove **every** `as Promise<…>` cast. Run `npx tsc --noEmit`. Expected: about 7 errors, in `getClientState`, `getSettings`, `getHotkeyState`, `getKeybinds`, `addTrigger`, `requestHotkeyPermission`/hotkey permission and `getNotifications`. Record the actual list in the commit message. If tsc reports a **missing or renamed field** (not just widening), stop: that is real drift, so report it, and do not narrow it away.
- [ ] **Step 2: Implement.** Add `SameKeys` and `narrow`. Wrap exactly the calls tsc flagged in `narrow<FeType>(App.X(...))`. Add a `SameKeys` guard for each flagged frontend type against its generated counterpart, recursing into nested DTOs (`Trigger`/`TriggerDTO`, `Stolen`/`StolenDTO`, the notification item type, `ClientStateSnapshot`/`models.ClientStateSnapshot`, and `Settings`/`SettingsDTO` including `audio`). Leave every other call un-cast, so its return type is inferred.
- [ ] **Step 3: Prove the guard bites.** Temporarily add `bogus: string;` to the frontend `Trigger` type in `store/settings.ts`. `npx tsc --noEmit` must fail with an error at the `Trigger` guard (`Type 'true' is not assignable to type 'false'`). Revert, and record the observed error text in the commit message.
- [ ] **Step 4: Verify.** `npx tsc --noEmit`, `npx vitest run` and `npm run build` are green. `grep -c "as Promise" src/shared/api/client.ts` must print `0`. `narrow` is the only cast site, so also run `grep -n " as " src/shared/api/client.ts` and confirm the only hit is inside `narrow`.
- [ ] **Step 5: Commit.** `git commit -am "refactor(api): alias generated binding types; compile-time drift guard on narrowed calls (FE13)"`

---

### Task 7: FE12 neutrality evidence — computed-style diff, origin/main vs branch

No repo changes. The harness lives in `frontend/.capture/` and is never committed.

- [ ] **Step 1: Build the harness.** Add `frontend/.capture/` to `.git/info/exclude`, then confirm `git status --porcelain` stays empty. Create:
  - `frontend/.capture/runtime-stub.ts`. It exports `Call.ByID(id, ...args)`, `CancellablePromise` (re-export `Promise`, adding a `cancel()` no-op), `Create` (the identity helpers the bindings use, plus any `Create.*` member they reference), `Events.On`/`Events.Off`, and `Window`. It resolves `ByID` through an id→method-name map, built at import from `bindings/.../internal/app/app.ts` by regex over `function (\w+)\(…\)…\$Call\.ByID\((\d+)` (read via `?raw`). It answers from `fixtures.ts`. After `Connect` resolves, it fires one `control:connection` = `"connected"` to the registered handler.
  - `frontend/.capture/fixtures.ts`. Self `CAPT` / coalition `blue`; 4 radios (one intercom, one disabled); 3 clients; a notification snapshot with one item each of info/warn/error; 2 profiles; 3 history rows; joystick state with 1 device; audio devices and state; settings with every toggle. Every Comms layout variant in `src/windows/comms/variants.ts` appears in `getCommsLayout` across the capture runs (one run per variant is fine).
  - `frontend/.capture/vite.capture.config.ts`. Extends the repo vite config with `resolve.alias: { "@wailsio/runtime": "<abs>/frontend/.capture/runtime-stub.ts" }`.
  - `frontend/.capture/dump.js`, to be injected. It first injects `*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}`. Then, for every element in DOM order, it records a stable path (tag + class + nth-child), the full `getComputedStyle(el)` plus `::before`, `::after` and (for inputs) `::placeholder`. It also records the `pulse` `CSSKeyframesRule` 50% opacity, read from `document.styleSheets`. Output is JSON.
- [ ] **Step 2: Capture "before".** Create a second worktree at `origin/main` (`git worktree add ../cl4-before origin/main`), copy `.capture/` into its `frontend/`, run `npm ci`, and generate the bindings. Then `npx vite build -c .capture/vite.capture.config.ts && npx vite preview -c .capture/vite.capture.config.ts`.
- [ ] **Step 3: Capture states** with Claude-in-Chrome at 1440×900, on both builds. On each, run `dump.js`, save `before/<state>.json`, and take a screenshot. States: Welcome (landing; guest form with empty inputs showing placeholders); Home; Players; History (empty search placeholder; rows); Profiles (name input placeholder); Settings × 8 sections; Settings → Keybinds with a KeyChip listening; StatusBar/TopBar with conn `reconnecting` (fire the event from the stub); the Comms popout (`/comms.html` or that window's entry) in every variant, plus a talker active; the Notifications popout with items.
- [ ] **Step 4: Capture "after"** from the branch worktree the same way, into `after/`.
- [ ] **Step 5: Diff.** A node script compares `before/*.json` with `after/*.json`. **Pass criterion:** zero differences except the allow-list in Global Constraints: (a) the `display` property on the 3 `.flex` paths; (b) every entry in the Settings Profiles, Notifications and Legacy captures (screenshot-reviewed instead); (c) the `pulse` 50% opacity, which must read `0.5` before and `0.4` after. Any other difference is a failure: fix the CSS in Task 5's files, re-run the gate commands, add a new commit, and re-diff. Paste the diff summary (state count, property count, allow-listed hits) into the PR body.
- [ ] **Step 6: Clean up.** `git worktree remove ../cl4-before`. Leave `.capture/` uncommitted, and confirm with `git status --porcelain` (empty).

---

### Task 8: Full verification, commit design + plan, open PR

**Files:**
- Create: `docs/superpowers/specs/2026-10-27-phase-6-9-cl4-frontend-cleanup-design.md` (copy of the finalized `design-CL4.md`)
- Create: `docs/superpowers/plans/2026-10-27-phase-6-9-cl4-frontend-cleanup.md` (copy of this plan)

- [ ] **Step 1: Frontend gate** (in `frontend/`): `npx vitest run` (all files pass; report the file and test counts verbatim), `npx tsc --noEmit` (no output), `npm run build` (exit 0).
- [ ] **Step 2: Client Go gate** (repo root): `go build -tags purego ./...`, `go vet -tags purego ./...`, `go test -race -count=1 -tags purego ./...`. All must exit 0. The Go code is unchanged, so this only confirms the branch. Report any failure verbatim. The known macOS flake `TestTXWriteFrameNeverBlocks` belongs to CL2. Note it if it appears, but do not fix it here.
- [ ] **Step 3: `npm ci` from clean.** Run `rm -rf node_modules && npm ci && npm run build` in `frontend/`. The lockfile must be consistent after the removals.
- [ ] **Step 4: Commit docs.** `git add docs/superpowers/specs/2026-10-27-phase-6-9-cl4-frontend-cleanup-design.md docs/superpowers/plans/2026-10-27-phase-6-9-cl4-frontend-cleanup.md && git commit -m "docs(phase-6.9): CL4 design and plan"`
- [ ] **Step 5: Push and open the PR.** `git push -u origin chore/cleanup-cl4`, then `gh pr create --base main --title "Phase 6.9 CL4: frontend cleanup & stack trim"`. The body has these sections: Summary (FE07, FE10, FE11, FE12, FE13, FE16, FE19); **Deliberate visual change**: the pulse keyframe now dips to 0.4 (design) instead of Tailwind's 0.5, user decision D1; the FE07 copy change; the dependency removals (7 packages, versions as in Tasks 1 and 5); the deleted files; the stack deviation (DOC records it in CLAUDE.md and the master spec); the Task 7 diff summary and screenshots; the manual-verification item below. End the body with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

## Manual-verification items (for the orchestrator to merge into `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`)

- [ ] **CL4 — Tailwind removal is visually neutral in a real Wails webview.** Build the app from `main` after CL4 merges, and compare against a pre-CL4 build on **Windows (WebView2)**, **Linux (WebKitGTK)** and **macOS (WKWebView)**. Check: Welcome (input placeholder colour, button font), Home/Players/History/Profiles, Settings (all 8 sections, number spinners in Audio), the Comms popout (all layout variants), the Notifications popout, and the History "Channel" label staying hidden. Expected: identical, except the pulse dot/listening chip now dims to 40% (deliberate) and the FE07 Settings copy. On WebKitGTK, pay particular attention to the `appearance` and form-control defaults (risk R1). On an engine older than `color-mix` support, the LCD cursor cell shows no tint (accepted, R5).
- [ ] **CL4 — Settings → Profiles & Layouts → OPEN PROFILES** navigates to the Profiles screen in the real app.
