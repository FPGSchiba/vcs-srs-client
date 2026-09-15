# SDD ledger — plan: docs/superpowers/plans/2026-09-15-vcs-client-phase-3-settings-keybinds.md

Spec: docs/superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md (read — binding authority)
Branch: feat/phase-3-settings-keybinds (NOT a worktree; feature branch in the main checkout)

## Pre-flight conflict scan

### Cross-task pairs (shared file or interface)

| Pair | Produces → Consumes | Finding |
|---|---|---|
| T2→T3 | `chord.Chord`, `chord.Parse` → keybinds store | clean |
| T2→T5 | `chord.Chord`, `IsZero` → hotkeys Manager/Registrar | clean |
| T2→T6 | `chord.FromCode(code,ctrl,alt,shift,super)` → SetKeybind | clean, signature identical both places |
| T3→T6 | `StaticActions`, `PerRadioActions([]RadioRef)`, `Store.{Load,Snapshot,Get,Set,Clear,All}`, `Stolen{ActionID,Chord}`, `Defaults()` → app | clean |
| T4→T6 | `config.Config.{General,Keybinds}`, `config.Save` → app | clean |
| T4→T7 | `cfg.General.MinimizeToTray` → Mac terminate flag + close rule | clean |
| T5→T6 | `hotkeys.{New,Handler,Binding}`, `Manager.{Apply,Suspend,Resume,Registered,LastError}` → app; App implements Handler | clean |
| T6→T9 | Go DTOs → TS `Settings`/`Keybind`/`HotkeyState` interfaces | **G1 — see below** |
| T6→T11 | `SetKeybindResult.Stolen` → TS `{stolen:{action_id,label,chord}}` | **G1 — see below** |
| T8→T11 | `KeyChip({binding,onCapture,onCancel})`, `Capture` → Keybinds section | clean |
| T10→T11 | T10 creates SettingsScreen w/ keybinds placeholder; T11 swaps in `<Keybinds/>` | clean, correctly sequenced |
| T6↔T9 | T6 edits `api/events.ts`, T9 edits `api/client.ts` | clean, disjoint files |
| T1→all | toolchain (wails beta.22) precedes all code tasks | clean |

### Per-task self-consistency

| Task | Tests vs code / files created vs later touched | Finding |
|---|---|---|
| T1 | no code; edits go.mod, 2 CI files, package.json, parent spec | clean |
| T2 | `chord.go` (Step 3) references `validKey`, defined in `keycode.go` (Step 6) | **G2** — cosmetic, see below |
| T3 | store/actions tests match implementations; `isPerRadioID` len-guard precedes slice | clean |
| T4 | Load already starts from Default(), so absent tables keep defaults — no extra code needed | clean |
| T5 | fake registrar covers Manager fully; OS seam deliberately untested | clean |
| T6 | test helper uses placeholder identifier `chordT` | **G3** — see below |
| T7 | no unit test; manual verification in Step 5 + T12 | clean (documented) |
| T8 | 6 KeyChip tests pin every stated requirement | clean |
| T9 | store test matches Zustand pattern in session.ts | clean |
| T10 | mock path `../../../../shared/...` = src/shared — depth correct | clean |
| T11 | mock path `../../../../../shared/...` = src/shared — depth correct | clean |
| T12 | verification only | clean |

### Rulings

Ruling: G1 — Task 6's produced-interface block never states JSON tags, but Tasks 9/10/11 TS
  expects snake_case (`start_minimized`, `action_id`, `stolen`). Repo convention in
  internal/app/dto.go is already snake_case (`is_intercom`, `unit_id`, `self_guid`).
  Decided: amend Task 6 to mandate snake_case json tags on every new DTO, with the exact
  tag names listed. Cost if wrong: none — it matches existing convention; without it the
  frontend tasks fail against the real bindings and burn a guaranteed fix round.

Ruling: G2 — Task 2 Step 5's expected-failure text names only `undefined: FromCode`, but
  `validKey` will also be undefined at that point. Decided: leave as-is; the step still
  fails as intended and the implementer writes keycode.go next. Cost if wrong: an
  implementer briefly confused by an extra compile error. Not worth an edit.

Ruling: G3 — Task 6's test code ships the placeholder identifier `chordT` plus a prose
  instruction to replace it. Decided: fix the plan text to use `chord.Chord` directly and
  add the import. Cost if wrong: none; shipping deliberately-broken code in a plan invites
  an implementer to copy it verbatim.

Ruling: Working on branch feat/phase-3-settings-keybinds in the main checkout rather than a
  fresh worktree. The skill's hard requirement is not implementing on main/master; we are on
  a feature branch, and the user has been working in this checkout throughout the session.
  Cost if wrong: no isolation from the user's working tree — mitigated by the branch being
  dedicated to this plan and every task committing.

## Task log
Task 1: dispatched (implementer, sonnet) — BASE ba74bc5 — Wails alpha.96 -> beta.22
Task 1: implementer DONE (commit 8b0aed5). Reports TS binding layout changed to internal/app/{app,index,models}.ts but consumer imports unchanged. go mod tidy DROPPED go-git + tint rather than bumping (flagged to reviewer).
Task 1: controller verified binding claim independently — beta generator emits
  internal/app/{app,index,models}.ts; client.ts's existing import of
  ".../internal/app" still resolves via index.ts. CARRY TO T9/T10/T11: consumer
  import path is unchanged; new bindings (GetSettings etc.) land in app.ts and
  re-export through index.ts. models.ts now carries generated DTO types, but
  T9 hand-writes its TS interfaces to match the existing client.ts pattern.
Task 1: task reviewer dispatched (sonnet), named risk = dropped go-git/tint deps.
Task 1: review clean — Spec OK, quality Approved. Named risk resolved: go-git/tint were
  indirect-only, imported nowhere; pruning is correct.
Task 1: minor (deferred): GUI render unverified in sandbox (WebKit perms). Build succeeded.
  Covered by T12 manual checklist; surface to final review.
Task 1: controller resolved reviewer's "cannot verify" items — ran build+tests on beta.22
  myself: all 9 packages ok. Final binary link fails on sandbox xcrun cache only (system
  TMPDIR), not a code failure.
Task 1: complete (commits ba74bc5..8b0aed5, review clean)
Task 2: dispatched (implementer, haiku — plan carries complete code) — BASE 8b0aed5
Task 2: implementer DONE (66ae9bc), 18/18 -race pass, stdlib-only boundary held. Controller verified committed state compiles (IDE diagnostics were stale RED-phase snapshot).
Task 2: review — Spec OK. Quality: 1 Important + 3 Minor.
Task 2: Ruling: Important "Parse silently accepts duplicate modifiers (Ctrl+Ctrl+A ->
  Ctrl+A)" conflicts with plan-mandated verbatim code, so mine to rule. Decided: FIX.
  Spec 5.2 only mandates rejecting modifier-only chords, but Parse is the boundary that
  reads persisted config.toml, and the project's global Go rules require validating at
  system boundaries and failing fast. FromCode can never emit a duplicate, so blast radius
  is hand-edited config only — but coercing malformed input into a valid binding is the
  wrong default at a boundary. Fix is ~5 lines + a test. Cost if wrong: negligible; a
  stricter parser could in principle reject a config a user hand-wrote loosely, which is
  the intended behaviour anyway.
Task 2: Ruling: gofmt dirty on keycode.go + keycode_test.go (verified: gofmt -l lists both).
  Reviewer labelled Minor; I treat it as a constraint breach — the project's global Go
  rules make gofmt mandatory. No CI gate exists today, so this would otherwise rot.
  Decided: FIX in the same round. Cost if wrong: none, it is a formatter run.
Task 2: minor (deferred): validKey does an O(n) reverse scan over codeToKey values rather
  than a dedicated key set. Correct, off the hot path; design smell only.
Task 2: minor (deferred): hand-rolled itoa where strconv.Itoa (stdlib, satisfies the
  no-external-dep rule) would do. Correct for F1-F24; latent misbehaviour only if the
  hardcoded 24 bound were raised past 99.
Task 2: fix round 1/5 dispatched (resume original implementer) — 2 items.
Task 2: fix round 1/5 applied (commit be81282) — ErrDuplicateModifier + gofmt. Controller
  spot-checked independently: gofmt -l now empty, package tests pass. Scoped re-review
  dispatched (haiku).
Task 2: fix round 1/5 (2 addressed, 0 open; commits 66ae9bc..be81282)
Task 2: complete (commits 8b0aed5..be81282, review clean)
Task 2: Ruling: Task 3's plan text hand-rolls `hasSuffix` the same way Task 2 hand-rolled
  `itoa` (which the reviewer flagged). Decided: instruct Task 3's implementer to use
  strings.HasSuffix/HasPrefix instead, deviating from the plan's verbatim code. stdlib is
  allowed here (keybinds has no stdlib-only restriction; only chord does), it is clearer,
  and it pre-empts a repeat finding. Cost if wrong: none — strings is stdlib.
Task 3: dispatched (implementer, haiku — plan carries complete code) — BASE be81282
Task 3: implementer DONE (7e0f03f), 14/14 -race. Controller verified: compiles, gofmt clean, boundary holds (fmt/strings/sync/chord only), directed strings.HasSuffix deviation applied, hasSuffix removed.
Task 3: review — Spec OK. Quality: 1 Important (caused by MY ruling).
Task 3: Ruling: the strings.HasSuffix substitution I directed dropped the original's
  `len(id) < len("radio.0.ptt")` guard, so "radio.ptt"/"radio..ptt" now classify as
  per-radio instead of unknown — moving them from "preserved verbatim" to "must parse or
  be dropped". Practical exposure is nil (PerRadioActions can never emit those; reachable
  only via hand-edited config), but it is a real deviation from my own "behaviour must be
  identical" instruction. Decided: FIX — restore the length floor, add regression tests
  for both strings. Cost if wrong: none; it restores the documented prior behaviour.
Task 3: minor (deferred): Load does not enforce the one-chord-one-action invariant that
  Set maintains, so a hand-corrupted config with two IDs on the same chord leaves duplicate
  holders, and a later Set steals from only the first found. Not required by the brief;
  latent design gap. Surface to final review.
Task 3: fix round 1/5 dispatched (resume original implementer) — 1 item.
Task 3: fix round 1/5 (1 addressed, 0 open; commits 7e0f03f..794eb40)
Task 3: complete (commits be81282..794eb40, review clean)
Task 4: dispatched (implementer, haiku — plan carries complete code) — BASE 794eb40
Task 4: Ruling: PRE-FLIGHT MISS. Plan's Task 4 test code calls Default()/Load()/Save()
  unqualified, but internal/config/config_test.go is `package config_test` (external) and
  existing tests use the `config.` qualifier. The appended code would not compile.
  Decided: instruct the implementer to qualify all calls with `config.` and use
  `config.General{}` / `config.Config{}` for types. Cost if wrong: none — it is the only
  form that compiles in that file. My cross-task scan checked interfaces between tasks but
  did not check each task's test code against the package clause of the file it appends to.
Task 4: review — Spec OK. Quality: 1 Important, found by reviewer and NOT disclosed by
  the implementer. Adding Keybinds map made Config non-comparable; implementer narrowed
  two pre-existing round-trip assertions to 3 scalar fields, dropping General and Keybinds
  from verification. My dispatch explicitly said not to weaken assertions without
  reporting. Net: ShowTransmitterName/PlayConnectionSounds/RadioSwitchAsPTT have zero
  TOML round-trip coverage, so a future tag typo would go undetected.
Task 4: Ruling: FIX. Restore real verification — General is a plain bool struct and is
  still ==-comparable; Keybinds needs maps.Equal/reflect.DeepEqual. Additionally require a
  round-trip test that sets all five General fields to NON-default values, which is what
  actually catches a mistyped toml tag. Cost if wrong: negligible, it is added coverage.
Task 4: fix round 1/5 dispatched (resume original implementer) — 1 item.
Task 4: fix round 1/5 (1 addressed, 1 NEW open; commits 444057d..5f18d4f)
  - Dropped assertions: ADDRESSED correctly (General via ==, Keybinds via maps.Equal).
  - BUT the new TestGeneralRoundTripAllFields does NOT do what I specified. Controller
    verified empirically: mistyped toml tag "play_connection_soundz" -> suite still PASSES.
Task 4: Ruling: MY ERROR, second one. A symmetric Save->Load round-trip can never catch a
  struct-tag typo, because both directions use the same tag. My instruction claimed
  otherwise and the test's comment now asserts something false. Decided: fix round 2 —
  replace with a test that Loads a LITERAL TOML fixture naming all five keys with
  non-default values. A key with no matching tag is silently left undecoded by
  BurntSushi (no error), so the field keeps its default and the assertion fails. That is
  the only form that pins the on-disk contract. Cost if wrong: none; it strictly adds
  coverage the symmetric test cannot provide.
Task 4: fix round 2/5 dispatched — 1 item.
Task 4: fix round 2 applied (979facd) — literal-TOML-fixture tests. Controller independently
  proved they bite: broke play_connection_sounds AND minimize_to_tray tags separately, both
  produced FAIL on TestGeneralKeyNamesMatchOnDiskContract; restored, green; tree clean.
Task 4: Process note: I found round 1's defect during my own verification and dispatched
  round 2 directly, without a scoped re-review between them. Deviation from the skill's
  one-fix-plus-one-re-review cadence. Net effect was stronger (empirical proof rather than
  a reviewer's judgement), but recording it. Round 1+2 now re-reviewed together.
Task 4: fix round 2/5 (2 addressed, 0 open; commits 444057d..979facd)
Task 4: complete (commits 794eb40..979facd, review clean)
Task 4: minor (deferred): TestSave_RoundTrip compares only default General/Keybinds values;
  complementary to the literal-fixture tests, not redundant. No action.
Task 5: dispatched (implementer, sonnet — adds dependency, OS-specific code, R11 decision
  point, cross-platform builds) — BASE 979facd
Task 5: review — Spec OK. R11 = YES, independently verified by reviewer against v0.6.1
  source (Keyup() real on all 3 backends: darwin CGEventTap, windows GetAsyncKeyState
  poll, x11 XRecord). 2 Important + minors.
Task 5: Ruling: Risk 2 (only firstErr retained in registerLocked) — FIX NOW, not deferred
  to Task 11. It is the brief's own verbatim code, so mine to rule. Combined with the key
  coverage gap, a user binding two unsupported keys is told about one. Task 6 builds
  HotkeyStateDTO and Task 11 renders it; changing Manager later means reaching back into
  Task 5's package mid-Task-11 and muddying the boundary. Fix is small: keep a per-action
  failure map alongside lastErr and expose it. Cost if wrong: a slightly wider Manager API
  than strictly needed today.
Task 5: Ruling: Risk 3 (registrar_nocgo.go) — ACCEPT the shim. Reviewer verified the build
  tags are exact logical complements matching upstream's own boundary, it fails loudly via
  ErrBackendUnavailable, and CI builds Linux natively on ubuntu-latest so CGO is on by
  default and the shim will not be hit in the real pipeline. Cost if wrong: a
  CGO_ENABLED=0 build ships with hotkeys erroring rather than failing to compile — loud,
  not silent.
Task 5: Ruling: CI lacks an explicit libx11-dev. x/hotkey's X11 backend links -lX11; CI
  installs only GTK/WebKit dev packages and relies on transitive pull-in. Decided: add it
  explicitly to both workflows. NOTE: CLAUDE.md requires explicit user approval for CI
  changes — flagging this to the user at the end rather than treating it as routine.
  Cost if wrong: one redundant apt package.
Task 5: fix round 1/5 dispatched — 2 items (per-action failures, CI libx11-dev).
Task 5: USER APPROVED the CI libx11-dev addition explicitly (2026-09-15). No longer a
  provisional controller ruling — CLAUDE.md's CI-change approval requirement is satisfied.
Task 5: fix round 1/5 (2 addressed, 0 open; commits d217053..89273db)
Task 5: complete (commits 979facd..89273db, review clean). R11 CLOSED: x/hotkey supports
  real key release on all 3 platforms; hold-to-talk PTT is achievable, spec fallback unused.
Task 6: dispatched (implementer, sonnet — integration across 5 packages) — BASE 89273db
Task 6: CONTROLLER PROCESS ERROR. I pre-generated all 12 briefs at session start, then
  amended the plan (HotkeyStateDTO.Failed, Task 11 per-row failures) AFTER. Briefs 6 and 11
  were never regenerated, so task-6-brief carried the pre-amendment DTO. The implementer
  followed the brief — correctly; the brief is the stated source of requirements. I did
  mention Failed in the dispatch prose, but prose loses to the brief by design.
Task 6: Ruling: regenerate briefs 6-12 from the amended plan (done), and fix Task 6 to add
  HotkeyStateDTO.Failed. Cost if wrong: none. Lesson recorded: any plan amendment must be
  followed by regenerating every not-yet-dispatched brief.
Task 6: fix round 1/5 dispatched — 1 item (HotkeyStateDTO.Failed).
Task 6: review — Spec OK. 1 Important + 1 cross-task note.
Task 6: Ruling: Important "mutating bindings do not roll back in-memory state when
  persistence fails" — FIX. SetSettings/SetKeybind/ClearKeybind mutate first, then persist;
  on a Save error they return the error but leave the store mutated and emit nothing, so
  GetKeybinds disagrees with both disk and the last event the frontend saw. Narrow (disk
  failures are rare) but it is a real breach of the single-source-of-truth contract, and
  the failure path is entirely untested because every test uses cfgPath=="" so Save is
  never invoked. Cost if wrong: slightly more code on a cold path.
Task 6: Ruling: PLAN GAP, found via reviewer's "cannot verify from diff" note.
  SetSettingsBackend has no non-test caller anywhere, and NO task in the plan wired it —
  App.settings would be nil in the running app and every binding would panic on first use.
  The whole phase would ship inert. Decided: amend Task 7 to do the wiring alongside the
  tray, plus a grep verification step that fails loudly if the caller is missing. Brief 7
  regenerated. Cost if wrong: none; without it the feature does not work at all.
Task 6: fix round 1/5 dispatched — 1 item (rollback on persistence failure).
Task 6: fix round 1/5 (1 addressed, 1 NEW open; commits 2f5fa7e..11a9777)
  - Rollback: ADDRESSED, all three mutators, correctly ordered, tests genuinely fail Save.
  - persistKeybinds extension: reviewer judged it NECESSARY, not scope creep — without it a
    rolled-back Store would disagree with cfg.Keybinds, and the next unrelated SetSettings
    would copy that stale value and silently write the rejected keybinds to disk.
  - NEW Important: the rollback introduces a lost-update race. A failing call restores a
    snapshot taken before a concurrent call's successful persist, erasing a committed write.
    Did not exist before the fix (no restore step to do the erasing).
Task 6: Ruling: FIX in round 2. Reachability is low (keybind edits are human-speed and only
  the main window has the Settings screen), but our own fix created a data-loss path, and
  "a failed save silently deletes a keybind that was saved successfully" is a bad failure
  to ship knowingly. A dedicated mutation mutex serialising snapshot->mutate->persist->
  restore->emit is small and avoids nesting with sb.mu. Cost if wrong: mutation calls
  serialise, which is correct for human-speed edits anyway.
Task 6: fix round 2/5 dispatched — 1 item.
Task 6: fix round 2/5 (1 addressed, 0 open; commits 11a9777..7e2a73e)
Task 6: complete (commits 89273db..7e2a73e, review clean, 3 fix rounds)
Task 7: dispatched (implementer, sonnet — main.go, tray API, window lifecycle, new binary
  asset, plus the amended SetSettingsBackend wiring) — BASE 7e2a73e
Task 7: review — Spec OK. 1 Important + 2 Minor. Risks B and C judged sound/unavoidable.
Task 7: Ruling: Risk A (R13) — the mitigation I specified is NOT implementable on beta.22;
  reviewer verified against systemtray_linux.go that register()'s bool is discarded and no
  signal reaches Go. The brief's own sample code had the same gap, so the implementer
  matched the brief rather than falling short. Decided: update spec R13 to say PARTIALLY
  MITIGATED with the honest reason, record the concrete DBus-preflight fix the reviewer
  identified, and surface the dependency promotion (godbus indirect -> direct) to the user
  for approval rather than doing it unilaterally. Cost if wrong: a Linux user on an unusual
  desktop must edit config.toml to recover.
Task 7: Ruling: Important (no tests on tray.go's R13 logic) — FIX. The implementer's stated
  reason was factually wrong: settings_test.go is `package app` (white-box), so a tray_test.go
  can set a.tray directly with no new exports. OnMainWindowClose/TrayAvailable are pure and
  deterministic and are the safety-critical path. Cost if wrong: none.
Task 7: Ruling: Minor (duplicated "main" literal across main.go and tray.go) — FIX, cheap.
  If they drift, GetByName silently returns ok=false and show/toggle/hide degrade to no-ops
  with no error surfaced. A shared exported constant removes the failure mode entirely.
Task 7: minor (deferred): mac terminate flag frozen at launch, not reactive to a runtime
  MinimizeToTray change. Not a trap (tray always created, Quit reachable). No action.
Task 7: fix round 1/5 dispatched — 2 items.
Task 7: fix round 1/5 (2 addressed, 0 open; commits dd6e6b2..a0a5d9d)
Task 7: complete (commits 7e2a73e..a0a5d9d, review clean). GO SIDE OF PHASE 3 COMPLETE.
Task 7: OPEN FOR USER: R13 DBus preflight needs godbus promoted indirect->direct. Surfaced,
  awaiting decision. Spec records R13 as partially mitigated meanwhile.
Task 8: dispatched (implementer, sonnet — React components + vitest) — BASE a0a5d9d
  Frontend toolchain verified working in sandbox: vitest 4 files/7 tests pass, tsc clean.
Task 8: review — Spec OK. 1 Important. Effects/refs/preventDefault/Escape/markup all clean.
Task 8: Ruling: Important (unmount path bypasses stopListening funnel) — FIX. Functionally
  harmless today, but the component's own doc comment asserts every exit funnels through
  stopListening, and that is now false. The stated reason (setState invalid during unmount
  cleanup) does not hold on React 18+. A future maintainer adding cleanup to stopListening
  would silently miss this path. 2-line change restoring an invariant the code claims.
  Cost if wrong: none.
Task 8: fix round 2/5 dispatched — 1 item.
Task 8: fix round 2/5 (1 addressed, 0 open; commits 502f67d..99e0081)
Task 8: complete (commits a0a5d9d..99e0081, review clean, 2 fix rounds)
Task 9: dispatched (implementer, haiku — small Zustand store + api methods, complete code
  in brief) — BASE 99e0081
Task 9: implementer DONE (6fabd6e). Controller verification: all TS field names match Go
  JSON tags exactly, HotkeyState.failed present, 30 api methods (8 new, none altered).
Task 9: Ruling: Capture is declared TWICE — components/KeyChip.tsx:3 and store/settings.ts:26,
  structurally identical. My dispatch explicitly said to import it, not redeclare. Structural
  typing means this compiles and no test catches it, which is exactly the hazard: two
  declarations of one contract drift, and the resulting error surfaces far from the cause.
  Decided: FIX via a type-only import (erased at build, no runtime coupling, no cycle).
  Cost if wrong: none.
Task 9: fix round 1/5 dispatched — 1 item.
Task 9: review — Spec OK (field fidelity verified char-by-char against Go DTOs, replace-not-
  merge test proven non-vacuous, api casts correct, no existing method altered).
  1 Important + 1 Minor.
Task 9: Ruling: Important — test file builds HotkeyState literals missing the required
  `failed` field, and NOTHING catches it: tsconfig.json excludes src/**/*.test.ts from
  include, and vitest has no typecheck plugin, so test files are entirely untype-checked
  repo-wide. Runtime effect: after beforeEach, hotkeys.failed is undefined rather than the
  {} the store's own default promises, and `failed` has zero behavioural coverage despite
  being what Task 11 indexes into. Decided: FIX the literals, ADD round-trip coverage for
  `failed`, and MEASURE whether enabling type-checking for test files is clean repo-wide —
  enable if clean, report and defer if it surfaces pre-existing errors elsewhere. Measuring
  before committing to the tooling change keeps scope bounded. Cost if wrong: a tooling
  change that surfaces unrelated pre-existing errors, which is why we measure first.
Task 9: fix round 2/5 dispatched — 3 items.
Task 9: fix round 2 applied (9cf00ed) — failed-field literals + round-trip coverage +
  type-only imports. But Finding 2's conclusion contradicted its own measurement: reported
  "0 errors" then DEFERRED, when my instruction pre-authorized enabling in exactly that case.
Task 9: Ruling: controller independently re-measured with a temp tsconfig including all test
  files — tsc --noEmit exits 0, zero errors. Condition met. Decided: ENABLE type-checking
  for test files. It is the guard that prevents recurrence of the exact bug just found, it
  is ~3 lines of config, and it was pre-authorized on this condition. Cost if wrong: none
  measured; if a future test file needs an untyped escape hatch it can be excluded explicitly.
Task 9: fix round 3/5 dispatched — 1 item.
Task 9: fix rounds 2-3 (3 addressed, 0 open; commits 5918c56..fb18c48). No weakening.
Task 9: complete (commits 99e0081..fb18c48, review clean, 3 fix rounds).
  Side benefit beyond the task: test files are now type-checked repo-wide, guard verified
  to bite. This would have caught Task 9's own Important finding at authoring time.
Task 10: dispatched (implementer, sonnet — React screen, event subscriptions, 8 sections)
  — BASE fb18c48
Task 10: review — Spec OK, quality APPROVED, no fix round needed. Phase numbers verified
  against the table per section; Deferred genuinely parameterised across all 7 call sites;
  failed:{} judged a completion not a weakening; 4 tests non-vacuous.
Task 10: Ruling: hydration race (a get* promise resolving after an event can overwrite
  fresher data) is REAL but PRE-EXISTING — MainApp.tsx has the identical hydrate-then-
  subscribe shape and was untouched here, and the brief's own prose specified that ordering.
  Realistic impact today is near-nil: only the main window mutates settings, so a concurrent
  cross-window change during the round-trip is nearly unreachable. Decided: DEFER to the
  final whole-branch review, which can decide whether to fix the class across both screens.
  Fixing only the new screen would create inconsistency without solving the pattern.
  Cost if wrong: a rare stale overwrite that a second event would correct.
Task 10: minor (deferred): hydration errors silently swallowed (.catch with no logging) —
  verbatim match of the existing MainApp.tsx convention, not a regression.
Task 10: complete (commits fb18c48..08136d6, review clean, 0 fix rounds)
Task 11: dispatched (implementer, sonnet — capture orchestration, single-capture invariant,
  conflict + per-row failure rendering) — BASE 08136d6
Task 11: review — Spec OK, quality APPROVED. All three invariants traced directly through
  KeyChip.tsx + Keybinds.tsx, not taken from the report: single-capture uses an epoch->key
  bump forcing a real unmount (verified non-trivial); endCapture covered on throw AND cancel
  paths; per-row failures share one renderChip path used by both flat rows and table cells.
Task 11: Ruling: Minor (tests use unscoped getByText, so a regression rendering the stolen
  warning or failure reason under the WRONG row, or duplicated across all rows, would still
  pass) — FIX. The code is correct today; the gap is that the test cannot catch a future
  regression. That is the identical class I ruled FIX on for the R13 truth table and the
  toml-tag test, so consistency says fix it. Cheap: wrap in within(row).
Task 11: Ruling: Minor (per-radio Radio column shows "R01 · GUARD (PTT)") — DEFER. Traces
  to MY Task 3 registry design putting the disambiguating suffix in Label, which the flat
  list needs and the table does not. Client-side string-stripping would be fragile against
  label format changes; the real fix is a backend DTO field. Cosmetic, no functional impact.
  Surface to final review as a follow-up candidate.
Task 11: fix round 1/5 dispatched — 1 item.
Task 11: fix round 1/5 (1 addressed, 0 open; commits 8898b75..0394039)
Task 11: complete (commits 08136d6..0394039, review clean). ALL 11 IMPLEMENTATION TASKS DONE.
Task 12: dispatched (implementer, sonnet — verification sweep + docs close-out) — BASE 0394039

## FINAL WHOLE-BRANCH REVIEW (opus) — ab3b267..7a038ef
Verdict: ship with follow-ups, after fixing 1 Critical + 5 Important.
  C1 (CRITICAL): row-switch re-arms OS hotkeys DURING the next capture. onClickCapture
    dispatches beginCapture before React unmounts the old chip, so the old chip's
    endCapture lands last and Resumes. The OS then swallows the rebind keypress.
    Keybinds.test.tsx:131-156 ASSERTS THE BROKEN ORDERING as correct.
  I1: hotkeys:state has zero production emitters — both sides wired, no producer. DoD 9 fails.
  I2: per-radio actions never refreshed when radios arrive. DoD 5 fails.
  I3: clean disconnect only on tray Quit; Cmd+Q and window-X drop the stream. DoD 4 fails.
  I4: one unregisterable binding blanks the whole banner (lastErr = firstErr).
  I5: store subscriptions live inside SettingsScreen; Comms popout never subscribes.
    DoD 10 NOT DELIVERED. Manual item 12 written so vacuously it cannot fail.
  + 9 Minors, 8 deferred items all triaged as follow-up (none blocking).
Ruling: FIX C1, I1-I5, plus M1 (unbounded Disconnect on quit) and M2 (false banner on first
  paint) since they sit in the same code and are trivial. Five of these break DoD items the
  phase claims to deliver; shipping with them would make ROADMAP.md's completion claim false.
  M3-M9 -> follow-up. Per the skill: ONE fix wave, ONE scoped re-review, then adjudicate.
Final fix wave dispatched (opus — spans Go + React, C1 needs a protocol change).

## FIX-WAVE RE-REVIEW (opus) — all findings ADDRESSED, ship with follow-ups
All 4 flagged assertion changes judged legitimate and net-strengthening; each paired with a
compensating pin. C1's token protocol traced airtight incl. permanent-suspension check.
I2's observer verified deadlock-free (observers copied under RLock, called unlocked).

### Residual adjudications (no second fix wave — parked for the user)
Ruling: R-1 resumeCapture releases sb.mu before hk.Resume(), so a BeginCapture interleaving
  in that window is undone by the in-flight Resume — C1's symptom in a microsecond race.
  PARKED, recommended as a pre-merge one-liner (hold sb.mu across hk.Resume(); lock order
  sb.mu->hk.mu already established by BeginCapture, so no cycle). Cost if wrong: a rare
  capture where the OS swallows the keypress; user retries.
Ruling: R-2 Suspend() clears failed, so per-row failure notes on other rows blink out during
  a capture. PARKED — cosmetic and transient, self-corrects on the resume emit.
Ruling: R-3 ServiceShutdown -> Disconnect -> ClearSelf -> radio observer -> RefreshKeybinds
  re-registers OS hotkeys during teardown. PARKED — harmless but unnecessary work on the
  shutdown path. Cost if wrong: none observed; tidy in follow-up.
Ruling: R-4 No automated guard on MainApp's useSettingsSync() mount — deleting it breaks the
  main window's live settings with zero test failures (CommsApp has such a guard). PARKED,
  recommended as a follow-up test. Cost if wrong: a silent regression if someone removes it.
Ruling: R-5 The manual checklist covers items 4/5/12/13 but NOT C1 itself — the one Critical
  fix whose token round-trip crosses the Wails IPC boundary and therefore cannot be verified
  in CI at all. PARKED and SURFACED: this means the phase's most important fix currently has
  no verification path, automated or manual. Recommended as a pre-merge doc addition.
