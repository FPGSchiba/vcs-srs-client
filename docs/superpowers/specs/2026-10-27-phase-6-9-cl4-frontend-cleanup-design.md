# CL4 — Frontend cleanup & stack trim (design)

**Status: approved.**

**Track:** CL4, Phase 6.9 umbrella §3 (`docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md`). **Repo:** client only. **Branch:** `chore/cleanup-cl4`, in a fresh worktree off **`origin/main` (f8ccb43)**, not off `docs/phase-6-9-umbrella-spec`. That branch predates the Dependabot bump #38, and its `frontend/package*.json` are older than main's.
**Baseline, re-measured on origin/main in a scratch export (2026-10-27):** `tsc --noEmit` clean, `vite build` clean, `vitest` 41 files / 498 tests green. The `frontend/bindings` regenerated from origin/main (`wails3 generate bindings -ts -f '-tags purego'`) are byte-identical to the working tree's.

## Decisions

| # | Question | Decision | By |
|---|---|---|---|
| D1 | Tailwind's `@keyframes pulse` (50% → 0.5) overrides the app's (50% → 0.4) | **The design prototype's 0.4 wins.** Removing Tailwind lets `components.css`'s own keyframe through. This is the one deliberate visual change in CL4. It is called out in the PR and allow-listed in the computed-style diff. | user (umbrella §9) |
| D2 | Copy for the Settings Notifications and Legacy stubs, which no phase owns | **"Not yet scheduled".** Notifications drops "Sound alerts" and points to General ("Play notification sounds"). "Server notifications" is marked as needing server support. | user (umbrella §9) |
| D3 | The 3 `className="flex …"` elements | No `display:flex` replacement. All three are flex items, so Tailwind's `display:flex` has no layout effect on them (§2 FE12). Their computed `display` is allow-listed in the diff. | worker, accepted by user |
| D4 | Where the preflight resets go | The relied-on subset goes into `global.css` inside `@layer base { … }` | worker, accepted by user |
| D5 | Popout shell (FE16) | A `.popout.native` CSS modifier, not a `PopoutShell` component | worker, accepted by user |
| D6 | Binding wrapper (FE13) | Alias the generated types. A `narrow<Fe>()` helper plus a compile-time key-drift guard covers the 7 drifting calls. | worker, accepted by user |
| D7 | Base branch | `origin/main` | user |
| D8 | `autoprefixer` and the direct `postcss` devDep | Removed as well (approval gate). Without them the build output is byte-identical. | worker (gate) |
| D9 | `StatusPill` | Deleted, not kept "for presence". `design/vcs/project/lib/atoms.jsx` is the source to re-port from. | worker |

## 1. Intent

Remove dead frontend code and dependencies that are declared but never used. Take Tailwind out of the stack **without changing what renders**, except for D1. The baseline is the app as it renders today (umbrella §9). Stop the Settings stubs from advertising phases that have already shipped. Make the binding wrapper show Go/TS type drift at compile time instead of casting it away.

## 2. Findings: re-verification

| ID | Status | Evidence (origin/main) |
|---|---|---|
| FE07 | holds | `SettingsScreen.tsx:35` profiles `phase={7}`, `:37` notif `phase={7}`, `:41-45` legacy `phase={4}`. All three phases have shipped. Two more facts: "Sound alerts" already exists as `play_notification_sounds` (`sections/General.tsx:79-90`, label "Play notification sounds"), and "Server notifications" is PROTO_GAPS #8 with no phase. The Misc stub `phase={10}` is still correct (ROADMAP Phase 10: auto-update, crash reporter). The `Deferred.tsx` doc comment (the "Task 10 brief" / "Keybinds placeholder pending Task 11" text) is stale. |
| FE10 | holds-with-correction | `shared/components/Placeholder.tsx` is imported only by its own test. `MainApp.tsx:23` imports the *different* `screens/Placeholder.tsx`, which is live and stays. `Pill.tsx` and `StatusPill.tsx` have zero importers. `store/windows.ts` `isOpen` has zero callers. `store/session.ts`: `error`/`setError` are never read and never called; `server` is **written** at `Welcome.tsx:59,77` (`setSessionServer`) and never read. So `Welcome.tsx` and `Welcome.test.tsx:22` are touched too, even though the umbrella file list does not name them. KeyChip: the only caller passes `binding=""`, so the **chord-render branch and the `binding` prop are unreachable**. The em-dash branch *is* reachable during the `addTrigger` await. Only the chord branch is dead. |
| FE11 | holds | No `@tanstack/*` or `zod` import anywhere in `frontend/`. Resolved versions: `@tanstack/react-query` 5.104.5, `@tanstack/react-router` 1.170.46, `zod` 4.6.7. |
| FE12 | holds-with-correction | The collision is real. The umbrella's "~10 elements" is wrong: **3** elements use `flex`: `History.tsx:118` and `Profiles.tsx:189` (`input flex`), and `NotificationsApp.tsx:117` (an empty spacer). All three are flex items (D3). Two items the umbrella did not foresee: a `.sr-only` dependency and the `@keyframes pulse` collision (D1). Full inventory in §4.4. |
| FE13 | holds-with-correction | 49 `as Promise<T>` casts plus 12 hand-declared DTOs. With every cast removed, `tsc` reports exactly **7** errors and **0** missing or renamed fields. Two kinds of widening cause all 7: (a) Wails emits Go maps as `{[k:string]?: T}`, where the TS side has `Record<string,T>` (`getClientState`, `getSettings` via `audio.effects`, `getHotkeyState`); (b) Go `string` vs TS literal unions (`Trigger.kind` in `getKeybinds`/`addTrigger`, the hotkey permission union, `NotifySeverity` in `getNotifications`). |
| FE16 | holds | Identical 9-property inline style at `CommsApp.tsx:199-211` and `NotificationsApp.tsx:75-87`. `.client` / `.client.active` (`components.css:8-24`) have zero TSX users. `.topbar{cursor:grab}` and `.topbar > *` are live and stay. |
| FE19 | holds | `TopBar.tsx:43-52` says "identity store is not wired yet … placeholder text", but the component reads `useSession(s=>s.self)` and renders the callsign/initials. The comment sits above `CONN_PILL`, not above `TopBar`. |

## 3. Files touched

`frontend/package.json`, `frontend/package-lock.json`. **Delete** `frontend/tailwind.config.ts` and `frontend/postcss.config.cjs`. `frontend/vitest.config.ts`: widen `css.include` to the three stylesheets. `frontend/src/shared/styles/{global,components}.css`. `frontend/src/shared/api/client.ts`. **Delete** `frontend/src/shared/components/{Placeholder,Placeholder.test,Pill,StatusPill}.tsx`. `frontend/src/shared/components/{KeyChip,KeyChip.test,TopBar}.tsx`. `frontend/src/shared/store/{session,windows}.ts`. `frontend/src/windows/main/screens/{Welcome,Welcome.test}.tsx`: one dead store write only; layout, classNames and the disabled SSO button are untouched. `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`: drop `binding=""`. `frontend/src/windows/main/MainApp.tsx`: passes `setView` into Settings. `frontend/src/windows/main/screens/settings/{SettingsScreen,SettingsScreen.test}.tsx` and `sections/Deferred.tsx`. `frontend/src/windows/comms/{CommsApp,CommsApp.test}.tsx`. `frontend/src/windows/notifications/{NotificationsApp,NotificationsApp.test}.tsx`. New tests: `frontend/src/shared/styles/styles.test.ts`, `frontend/src/stack.test.ts`, `frontend/src/shared/deadcode.test.ts`.

Manual-verification items go back to the orchestrator in the plan's final section. The orchestrator merges them into `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`. CL4 does not edit that file.

None of the extra files overlap another wave-1 track. CL1 (wave 3) edits `Welcome.tsx` after CL4 has merged.

## 4. Approach per finding

### 4.1 FE11: drop unused runtime deps
`npm uninstall @tanstack/react-query @tanstack/react-router zod` in the worktree off origin/main, so the lockfile diff contains only removals. `npm ci && npm run build` must still pass.

### 4.2 FE10: dead code
- Delete `shared/components/Placeholder.tsx` and its test, `Pill.tsx` and `StatusPill.tsx` (D9).
- `windows.ts`: drop `isOpen` and the `get` parameter.
- `session.ts`: drop `error`, `setError`, `server` and `setServer`. Drop the `setSessionServer` selector and call in `Welcome.tsx`, and fix the `Welcome.test.tsx` setState. CL1's "remembered server address" lives in `config.toml` (umbrella §10), not in this store.
- `KeyChip`: remove the `binding` prop and the chord branch, so the non-listening render is always the em dash. Update the one caller and `KeyChip.test.tsx`: the "renders the bound chord" test is deleted, and the Escape test arms via the em dash. The em-dash re-arm during the `addTrigger` await stays exactly as it is.

### 4.3 FE07: Settings stubs (D2)
- `Deferred`'s `phase` becomes optional. With a `phase`, the copy is "Arrives in Phase N", as today. With no `phase`, the stub renders the state-title **"Not yet scheduled"**. A new optional `note?: string` renders one `cap-dim` line under the title.
- **Profiles & Layouts**: no longer a `Deferred`. It renders a `Panel` titled "PROFILES & LAYOUTS" with the text "Profiles and layouts are managed on the Profiles screen." and an `OPEN PROFILES` `.btn` that calls `onNavigate("profiles")`. `SettingsScreen` takes an optional `onNavigate?: (view: string) => void` prop. In `MainApp`, `SCREENS` becomes a function of `setView` (no router, as today).
- **Notifications**: `<Deferred items={["Alert rules", "Server notifications (needs server support)"]} note="Notification sounds: General → Play notification sounds." />`. "Sound alerts" is dropped.
- **Legacy**: `<Deferred items={["Legacy SRS audio engine", "Legacy overlay", "Disable hardware acceleration"]} />` with no phase, so it says "Not yet scheduled".
- **Misc**: unchanged (`phase={10}`).
- Rewrite the stale `Deferred.tsx` doc comment to describe both modes.

### 4.4 FE12: remove Tailwind, rendering-neutral except D1
**What Tailwind contributes today.** This list comes from the *built* CSS, so it is exhaustive. Tailwind v4 emits only the candidates it found, and every entry was checked against JSX.

| Built layer | Effect on the app | Action |
|---|---|---|
| `utilities` (46 utilities, mostly false positives from comments and strings) | Real `className` uses: **`sr-only`** (`History.tsx:84`, not defined in `components.css`); **`flex`** ×3 (no effect, D3); `gap-2..6` (unlayered `components.css` beats them already); the rest only in the dead `shared/components/Placeholder.tsx` | Add `.sr-only` to `components.css` with Tailwind's exact declarations: `position:absolute; width:1px; height:1px; padding:0; margin:-1px; overflow:hidden; clip-path:inset(50%); white-space:nowrap; border-width:0`. Copy these from the baseline built CSS, which is authoritative if it differs. |
| `base` (preflight, 50 rules) | See the list below | Port the relied-on subset into `global.css` inside `@layer base { … }` (D4), with the attribution comment "adapted from Tailwind CSS v4 preflight, MIT". |
| `theme` | Nothing references Tailwind theme variables | Drop |
| `properties` + 35 `@property --tw-*` | Used only by the dropped utilities | Drop |
| `@keyframes pulse { 50% {opacity:.5} }` | Overrides `components.css:607` | Dropped with Tailwind, so the design's 0.4 applies (**D1, user decision**). This is a deliberate change. |
| lightningcss lowering of `color-mix` (`button.lcd-digit[data-cursor=true]`) | Identical on any engine with `color-mix` (Chromium ≥111/WebView2, Safari ≥16.2, WebKitGTK ≥2.40). On older engines the cursor cell loses its tint. | Accept, and record as risk R5 |

**Relied-on preflight resets.** The rendered elements are `div span svg path circle rect line ellipse button input[text,password,number,range] select option label table thead tbody tr th td ul li fieldset nav main b`. The resets to port, values exactly as in the baseline built CSS:
1. `*, ::after, ::before, ::backdrop { box-sizing:border-box; border:0 solid; margin:0; padding:0 }`
2. `html` minus font-family/line-height: `-webkit-text-size-adjust:100%; tab-size:4; font-feature-settings:normal; font-variation-settings:normal; -webkit-tap-highlight-color:transparent`
3. `b, strong { font-weight:bolder }`
4. `table { text-indent:0; border-color:inherit; border-collapse:collapse }`
5. `ol, ul, menu { list-style:none }`
6. `img, svg, video, canvas, audio, iframe, embed, object { display:block; vertical-align:middle }`
7. `button, input, select, optgroup, textarea, ::file-selector-button { font:inherit; font-feature-settings:inherit; font-variation-settings:inherit; letter-spacing:inherit; color:inherit; border-radius:0; background-color:transparent; opacity:1 }`
8. `button, input:where([type=button],[type=reset],[type=submit]), ::file-selector-button { appearance:button }`
9. `::placeholder { opacity:1 }`, plus the `@supports` block giving `::placeholder { color: color-mix(in oklab, currentcolor 50%, transparent) }` with the `currentcolor` fallback as built
10. `::-webkit-inner-spin-button, ::-webkit-outer-spin-button { height:auto }`
11. `[hidden]:where(:not([hidden=until-found])) { display:none !important }`, as built

Dropped as unreachable, and listed in a comment inside the block: `hr`, `abbr`, `h1-h6`, `a`, `code/kbd/samp/pre`, `small`, `sub/sup`, `progress`, `summary`, `textarea{resize}`, `img,video{max-width;height}`, the date/time/search/file pseudo-elements, `:-moz-*`.

**Removal.** Delete `@import "tailwindcss"`, the `@config` line *and its comment block*. Deleting only the `@config` line leaves an unclosed comment. Delete `tailwind.config.ts` and `postcss.config.cjs`, and uninstall `tailwindcss`, `@tailwindcss/postcss`, `autoprefixer` and `postcss` (D8). `postcss` stays in the tree transitively via `vite`.

**Evidence of neutrality.**
- *Harness:* kept in `frontend/.capture/`, excluded through `.git/info/exclude` and never committed. A `vite.capture.config.ts` aliases `@wailsio/runtime` to a stub. The stub answers `Call.ByID` from a fixture table (connected session, 4 radios, 3 clients, one notification of each severity, profiles, history rows, joystick/audio state) and makes `Events.On` a no-op, except for one synthetic `control:connection = "connected"` after `Connect`. The same harness and fixtures run against builds of origin/main ("before") and of the branch ("after"), served by `vite preview`.
- *Capture:* Claude-in-Chrome at 1440×900. Covers every screen, every Settings section, each Comms layout variant, a Comms talker, the Notifications popout, and the states that use pulse, placeholder or sr-only.
- *Pass criterion:* an injected script freezes animations and transitions, then dumps `getComputedStyle` for every element and for `::before`, `::after` and `::placeholder` to JSON. Before and after must be identical except for this allow-list: (a) the `display` of the 3 `.flex` elements; (b) the FE07 Settings sections Profiles, Notifications and Legacy, which are compared by screenshot only; (c) the `pulse` keyframe's 50% opacity, 0.5 → 0.4 (D1), read from the CSSOM. The JSON diff is the gate. Screenshots go in the PR.
- Chrome is not WebView2, WKWebView or WebKitGTK, so the real-webview check goes on the manual checklist.

### 4.5 FE13: binding wrapper (D6)
- 11 hand DTOs become `export type X = models.Y` aliases of the generated classes: `RadioDTO`, `RadioInfoDTO`, `ClientInfoDTO`, `BuildInfo`→`BuildInfoDTO`, `VoiceStateDTO`, `LayoutBlock`→`ProfileBlockDTO`, `LayoutWindow`→`ProfileWindowDTO`, `Layout`→`LayoutDTO`, `ProfileSummary`→`ProfileSummaryDTO`, `ProfileState`→`ProfileStateDTO`, `HistoryEntry`→`HistoryEntryDTO`. Consumer import names do not change. **`ClientStateSnapshot` stays frontend-shaped** (`Record<string,…>` built over the aliased element types), because `getClientState` is one of the narrowed calls and its consumers rely on non-optional map values.
- The casts that show no drift are removed, and return types are inferred (`CancellablePromise<T>` is a `Promise<T>`).
- The narrowed calls keep their frontend types and go through one helper, `narrow<Fe>()`. Next to it sits a compile-time drift guard: a type-level assertion that the key sets of the generated and frontend types are equal, recursively for nested DTOs (`Trigger`, `Stolen`, `NotifyItem`, the snapshot element types). An added, removed or renamed Go field then fails `tsc`.

### 4.6 FE16: popout shell (D5)
Add `.popout.native { position:static; width:100%; height:100%; border:none; border-radius:0; box-shadow:none; min-width:0; min-height:0 }` next to `.popout`. Both popout roots become `className="popout native"` with no inline style. Delete `.client` and `.client.active`. `.popout.active`, `.popout-resize` and `.popout-chrome.dragging` stay.

### 4.7 FE19
Rewrite the doc comment to state what the code does (identity comes from `useSession().self`, falling back to "GUEST" / "—") and move it above `export function TopBar`.

## 5. Tests first (TDD)

| Test | Fails before, passes after |
|---|---|
| `src/stack.test.ts` (new; reads `package.json?raw`) | none of the 7 removed packages appear in `dependencies`/`devDependencies` |
| `src/shared/deadcode.test.ts` (new; `import.meta.glob` + `@ts-expect-error`) | the deleted component files are gone; `useSession`/`useWindows` no longer expose the removed members (red through `tsc`) |
| `src/shared/styles/styles.test.ts` (new; `?raw`) | no Tailwind directives; `.sr-only`; the `@layer base` resets; `.popout.native`; no `.client` rule; exactly one `@keyframes pulse`, with 50% at `opacity: 0.4` |
| `SettingsScreen.test.tsx` (changed) | Profiles pointer and `onNavigate("profiles")`; Notifications/Legacy show "Not yet scheduled"; Notifications has no "Sound alerts" and has the General pointer and "needs server support"; only Misc says "Arrives in Phase", with 10 |
| `CommsApp.test.tsx`, `NotificationsApp.test.tsx` (changed) | root has classes `popout native` and no `style` attribute |
| `KeyChip.test.tsx` (changed) | chord test removed; nothing passes `binding` |
| FE13 | a red step adds a field to one frontend type and shows that `tsc` fails, then reverts it |

Full gate: `npx vitest run`, `npx tsc --noEmit`, `npm run build`, the computed-style diff, and the client Go suite once before the PR.

## 6. Out of scope
Phase 10's React Doctor / SonarCloud. Re-styling toward the prototype beyond D1. Deleting other dead prototype CSS. Keyboard, aria and live-region work (CL3). The em-dash re-arm behaviour. Widening store types. Committing the capture harness. The CLAUDE.md and master-spec stack-line edits (DOC records the deviation).

## 7. Behaviour changes (user-visible)
- FE07: Settings copy for Profiles (plus a navigation button), Notifications and Legacy.
- FE12 / D1: the pulse animation on `.kbd.listening`, `.dual-pill .d.warn`, `.conn-pill.warn .dot` and the `components.css:251` user dips to 0.4 opacity instead of 0.5 (the design value).
- FE12: the `lcd-digit` cursor tint on engines without `color-mix` (R5). Nothing else is intended.
- Nothing is wire-visible.

## 8. Approval-gate items
- Remove npm deps: `@tanstack/react-query@5.104.5`, `@tanstack/react-router@1.170.46`, `zod@4.6.7`.
- Remove npm devDeps: `tailwindcss@4.3.3`, `@tailwindcss/postcss@4.3.3`, `autoprefixer@10.6.1`, `postcss@8.5.31` (the direct entry only).
- Delete tracked files: `frontend/tailwind.config.ts`, `frontend/postcss.config.cjs`, `frontend/src/shared/components/{Placeholder,Placeholder.test,Pill,StatusPill}.tsx`.
- User-visible: FE07 copy, and the D1 pulse depth.
- Architecture deviation from the master spec's stack (no Tailwind, TanStack or zod). DOC records it.

## 9. Risks
- R1. The capture runs in Chrome, which differs from the real webviews, above all WebKitGTK form-control and `appearance` defaults. This goes on the manual checklist.
- R2. A later element relies on a dropped preflight reset. Mitigation: the comment in the `@layer base` block names the dropped rules.
- R3. Fixture gaps hide a state from the diff. Mitigation: the plan enumerates the states, and the fixtures drive every variant in `windows/comms/variants.ts`.
- R4. Merge order: CL3 (wave 2) rebases onto `CommsApp.tsx`/`SettingsScreen.tsx` after this track.
- R5. On an engine without `color-mix`, the `lcd-digit` cursor loses its tint.
