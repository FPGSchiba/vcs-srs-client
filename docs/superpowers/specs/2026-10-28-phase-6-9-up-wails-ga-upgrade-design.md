**Status: approved by the user on 2026-10-28** (pass 3, plus U7). All six §10 questions were answered by the user on 2026-10-28; the decisions are in the U1–U6 rows below and marked in §10. This is read-only design research. Nothing was modified in either repo, and nothing was committed or pushed.

# Phase 6.9 / UP: Wails v3 GA upgrade (client + server)

**Repos:** `vcs-srs-client` (`origin/main` `e7c7c30`) and `vcs-srs-server` (`origin/main` `09f9180`). Suggested branches: `chore/cleanup-up` in each, cut from `origin/main`.
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md:240` (track UP). Gates: **dependency, ci-cd, cross-repo**.
**Evidence basis:** two real trial bumps in throwaway detached worktrees (removed and pruned afterwards), three area mappings with independent verifier passes, a critic pass that saw only the first two areas, and a completeness re-run over all three areas (appendix). Verifier corrections are applied below. Refuted citations are dropped or replaced (§3, "Citation corrections"). gopls was not used. Every compile claim rests on `go build` / `go vet`.

## Decisions (D1–D7 proposed; U1–U6 decided by the user on 2026-10-28; design approval pending)

| # | Decision | Source |
|---|---|---|
| D1 | Target **`github.com/wailsapp/wails/v3 v3.0.0`** and **`@wailsio/runtime 3.0.0`** in both repos. Not the nightly. | §2 |
| D2 | **Zero source changes in the client.** The bump is go.mod/go.sum + package.json/lock + CI. | client probe, §4.1 |
| D3 | Server source change limited to the **typecheck fix in `frontend/src/pages/ClientList.tsx`** (`:11` state type, `:21`/`:39` setters, guard on the `Object.entries` at `:47`, whose `client` value is passed at `:48`). No Go change. Server non-source edits that UP itself makes necessary: `build/Taskfile.yml:74` (`--host 127.0.0.1`, §3 #20), the stale `x/tools v0.46.0` comment at `.github/actions/generate/action.yml:20`, and the stale install text at `README.md:9-13`. | server probe, re-run |
| D4 | The wails3 CLI version is derived from go.mod with `go list -m`, and a buildcheck test forbids a literal `wails3@v…` pin in workflows. | §5.3 |
| D5 | Client and server land as **separate PRs**, client first. Neither depends on the other. | §6.3 |
| D6 | New GA features and template hardening are not adopted (GlobalShortcut, SingleInstance-as-feature, `exit_policy`, `dev_mode`, the post-GA nightly tray fixes, the GA darwin Taskfile steps from https://github.com/wailsapp/wails/pull/6225 and /6237). Any item in §10 that would add an option is decided by the user, not assumed. | umbrella:240 |
| D7 | `go mod tidy` runs **after** codegen in both repos (client `buf generate`; server 3× protoc + gqlgen). On the server, tidying without the generated packages only bumps go-git to v5.19.2 / go-billy v5.9.0 / circl v1.6.3 instead of dropping them, and the SEC Dependabot closure (§7) would not happen. | build verifier, re-run |
| U1 | Q1 = A: exact `"3.0.0"` in both `package.json` files, enforced by `TestWailsRuntimeMatchesGoModule` in the client. | user |
| U2 | Q2 = A: follow the GTK4 default. Hand-edit `build/linux/nfpm/nfpm.yaml:27-29` to the GA package names (deb `libgtk-4-1`, `libwebkitgtk-6.0-4`, plus the rpm and archlinux overrides). No new CI job; "server GUI never built in CI" stays a follow-up. | user |
| U3 | Q3 = A: accept the new behaviour (a second Linux instance fails on port bind). No SingleInstance. | user |
| U4 | Q4 = A: accept an opaque macOS admin window. No `private_mac_apis`. | user |
| U5 | Q5 = A: keep `GO_DEPS_EXCLUDE` for Wails in both repos and reword its comment. Wails is bumped by hand, Go and npm together. `TestGoDepsWorkflowShape` is unchanged. | user |
| U6 | Q6 = B: adopt the `0409` key in UP: client `build/windows/info.json:6` plus `buildcheck_test.go:206` (test first); server `build/windows/info.json:6`. The user chose this over D6's exclusion. | user |
| U7 | A separate weekly Wails check in each repo, added after design review. A scheduled workflow (plus a manual trigger) compares the Wails version in go.mod and @wailsio/runtime in package.json with the latest Wails release, using the Go module proxy and the npm registry. If a newer release exists, it opens or updates one issue titled "Wails vX available". The issue links the changelog and flags whether the release fixes a security advisory. Bumps stay manual, with Go and npm together (U5). This is a CI change, approved with the design. | user |

## 1. Intent and scope

Move the client from `v3.0.0-beta.22` and the server from `v3.0.0-alpha.43` to Wails `v3.0.0` GA. Bring the `@wailsio/runtime` npm package in step. Regenerate the bindings and fix whatever breaks. Stop duplicating the wails3 CLI version in workflows: derive it from go.mod. Resolve the "Wails excluded until UP lands" carve-outs in both repos' weekly Go-deps workflows: the umbrella assumed they would be lifted, but the re-run found that lifting them conflicts with the exact runtime pin (R11), so this is now Q5.

**In scope:** the dependency deltas in §6.1, the CI edits in §6.2, the server `ClientList.tsx` typecheck fix, the server dev-loop `--host` fix (§3 #20), stale-comment and README rewording (client `release.yml`, server `action.yml`/`README.md`), and additions to the existing manual-verification checklists.

**Out of scope:**
- Adopting any new GA feature or template hardening (D6). The macOS `wails3 tool prepare-macos-output` / `rm -rf` / `xattr -cr` / `codesign --verify --deep --strict` Taskfile template steps come from https://github.com/wailsapp/wails/pull/6225 (follow-up /6237; changelog lines at `docs/mpress/content/changelog.md` v3.0.0 tag). They are a **fix for local macOS builds whose output lies in a FileProvider-synced folder** (iCloud `~/Documents`), where Finder metadata makes ad-hoc codesign fail. CI runners do not build in such a folder, and the client's checked-in `build/darwin/Taskfile.yml` does not need them for the unsigned build (`release.yml:278`). They stay out; a macOS developer who hits the codesign failure can port them later.
- The post-GA nightly `v3.1.0-nightly.20261027` (Linux tray tooltip, https://github.com/wailsapp/wails/pull/6275, https://github.com/wailsapp/wails/pull/6284).
- A server `ServiceShutdown` that stops the gRPC and voice listeners on SIGINT/SIGTERM (B21-01; a new feature).
- The server frontend's ~112 pre-existing tsc errors (§4.2). Adding a tsc gate to server CI is not proposed.
- grpc and quic-go on the server, and x/net v0.62.0 on the client. These belong to SEC / the weekly Go-deps track (§7).

## 2. Target version

`go list -m -versions github.com/wailsapp/wails/v3` (probe) lists `… v3.0.0-beta.34 v3.0.0 v3.1.0-nightly.20261027`. `npm view @wailsio/runtime dist-tags` gives `{ latest: '3.0.0', nightly: '3.1.0-nightly.20261027' }`. **There is no 3.0.x fix release.**

**Recommendation: `v3.0.0`**, tagged at https://github.com/wailsapp/wails/commit/7b11001b92b712210b27d8cb53430e83f67ad8e4 on 2026-10-26 (release notes: https://github.com/wailsapp/wails/releases/tag/v3.0.0). Reasons:
- It is the only GA release. The 3.1 nightly is a pre-release of the next minor and carries new behaviour.
- Its go.mod needs `go 1.26.0` / `toolchain go1.26.10`; both repos are on Go 1.27 (client `go.mod:3,5`), and the release notes recommend 1.27.3.
- Both trial bumps built, vetted and tested on it (§4).

**Caveat:** the tag is two days old. If a `v3.0.1` appears before implementation, re-run the §4 probe on it and take it only if its changelog is fixes-only.

**npm pairing rule.** The v3.0.0 release notes say: "The @wailsio/runtime npm package always has the same version as the Wails release. Pin an exact version in production builds." Both repos already lock `3.0.0-beta.34` against a different Go version (client `frontend/package.json:17`, `package-lock.json:1177-1179`; server `frontend/package-lock.json:2800-2803`). npm `3.0.0-beta.34` and `3.0.0` have **byte-identical `dist/` and `types/`** (verifier: `npm pack` of both, `diff -rq`; only `package.json` version and devDependencies differ). So the npm step is a version/lockfile bump with no runtime change in either repo. It does **not** "restore a matched JS runtime" (an earlier claim, refuted by the verifier). Pin form: see Q1.

## 3. Breaking / behaviour-change table

Impact classes: **breaks-typecheck** (fails `tsc`), **semantic** (generated type changes meaning, no error), **behaviour** (compiles, behaves differently at runtime), **packaging/CI**, **none**.

| # | Change (Wails) | Repo | Our locations | Impact | Required change | Evidence |
|---|---|---|---|---|---|---|
| 1 | Binding generator emits Go maps as `{ [_ in string]?: T }` (optional values) instead of `{ [_: string]: T }` | server | Go: `app/clients.go:15,20`, `state/settings.go:82,87`. TS: `frontend/src/pages/ClientList.tsx:11` (`useState<Record<string, ClientState> \| null>`), `:21` `setClients(clients.Clients)`, `:39` `setClients(result?.Clients ?? {})`, `:47` `Object.entries(clients).map(…)`, `:48` `client={client}` | **breaks-typecheck** (2 × TS2345). `vite build` and vitest still pass; server CI runs neither tsc nor a build (`test.yml:62-83`), so CI stays green. Type-level only: `Clients` is `map[string]state.ClientState` (value type, `app/clients.go:15`), so Go never sends an `undefined` value at runtime. | Widen `:11` to `Partial<Record<string, ClientState>> \| null` and narrow at `:47` with `.filter((e): e is [string, ClientState] => e[1] !== undefined)`. **Re-run verified:** with exactly this change, `tsc --noEmit -p .` against the GA bindings returns to the identical 112-error baseline set (sorted diff empty). | Probe tsc diff; https://github.com/wailsapp/wails/pull/5331 (#4943), alpha.68 changelog entry in `docs/src/content/docs/changelog.mdx` at tag v3.0.0-alpha.78 |
| 2 | `time.Time` binds as `string` (default `-time-type string`, v3.0.0 `internal/flags/bindings.go:15`) | server | generated `state/models.ts` `LastUpdate`; mock `frontend/src/test/mocks/state.ts:102` (`string \| null`) | **semantic**. `LastUpdate` default goes from `null` to `"0001-01-01T00:00:00.000Z"`; the `bindings/time` package disappears (models 25 → 24). No non-test `src` use found by grep. | None. Note in the PR. | https://github.com/wailsapp/wails/pull/5398 |
| 3 | New generated `bindings/events/index.ts` (side-effect import of `internal/eventcreate.js`; no constants, since neither repo calls `RegisterEvent`) | both | client: gitignored `frontend/bindings/` (`.gitignore:14`), outside `tsconfig` `include: ["src"]`; server: no collision with `bindings/github.com/FPGSchiba/vcs-srs-server/events` (`ClientEntry.tsx:3`) | **none** | None. All other client output is byte-identical (59 methods, 36 models). | https://github.com/wailsapp/wails/commit/6702ff837c72a7907d6c74e90e639cdd0437a89a (#6307) |
| 4 | Windows tray handles `WM_CONTEXTMENU` (NOTIFYICON_VERSION_4); `openMenu` falls back to the cursor position when `Shell_NotifyIconGetRect` fails (overflow flyout) | client | `internal/app/tray.go:27-37`, `:35` Quit, `:52-58` showMainWindow, `:80-91` quit | **behaviour (a fix).** Under beta.22 the right-click tray menu (Show VCS / Settings / Quit) never opened on Windows (beta.22 `systemtray_windows.go:463,619`). | None. Add a manual check (§9). | https://github.com/wailsapp/wails/pull/6162; v3.0.0 `systemtray_windows.go:58-64,466-469` |
| 5 | Linux: opening the tray menu no longer fires the click handler | client | `tray.go:37` OnClick → `toggleMainWindow` `:62-73` | **behaviour (a fix).** Under beta.22 every Linux right-click also hid/showed the main window. | None. Manual check (§9). | https://github.com/wailsapp/wails/pull/6018 |
| 6 | Go → JS events go through an ordered FIFO mailbox; oversized payloads served host-side | server (client already has it at beta.22) | `app/app_gui.go:101,121` (`Event.EmitEvent`, including the `logs/entry` stream) | **behaviour.** alpha.43 started a goroutine per emit (alpha.43 `events.go:159-166`), so delivery was already unbounded and unordered. The change is that the log stream now arrives **in order**. | None. | https://github.com/wailsapp/wails/pull/5851, https://github.com/wailsapp/wails/pull/5930 |
| 7 | The default signal handler is now started | server (client already has it) | `main.go:22-27,82`; no `ServiceShutdown` in `services/*.go` | **behaviour.** SIGINT/SIGTERM to the GUI server now goes through Wails `Quit` ("Quitting application...") instead of killing the process. Listeners are still not stopped gracefully, as before. | None (graceful stop is out of scope). | https://github.com/wailsapp/wails/pull/6098; alpha.43 vs v3.0.0 `signal_handler_desktop.go:19` |
| 8 | Linux GtkApplication created `G_APPLICATION_NON_UNIQUE` (GTK4 and GTK3) | server | `main.go:30-41` (no `SingleInstance`), `main.go:50` (listeners autostart) | **behaviour.** A second server GUI on Linux used to activate the first; it now starts fully and fails to bind its ports. | **Q3.** | https://github.com/wailsapp/wails/pull/5971; v3.0.0 `linux_cgo.h:22`, `linux_cgo_gtk3.go:29` |
| 9 | Linux GUI default is GTK4 / WebKitGTK 6.0 (`linux && cgo && !gtk3`; `-tags gtk3` until v3.1) | server (client CI already installs GTK4: `.github/actions/linux-gui-deps/action.yml:37`) | `build/linux/nfpm/nfpm.yaml:28-29` (declares gtk3 / libwebkit2gtk); no server CI job builds the GUI (`test.yml:39`, `release.yml:32-35`, `go-deps.yml:34` are all `headless`) | **breaks-build (Linux GUI, local only) + packaging.** GA's cgo `pkg-config: gtk4 webkitgtk-6.0` (v3.0.0 `linux_cgo.go:17`) fails on a dev host with only the GTK3 dev packages, which alpha.43 needed (`linux_cgo.go:19`). The probes ran on Windows and server CI is headless, so neither could hit it. The nfpm deps (`gtk3`, `libwebkit2gtk`) are **already invalid today**: they are not Debian package names and are older than even the alpha.43 template (`libgtk-3-0`, `libwebkit2gtk-4.1-0`). No server job builds or ships this package. | **Q2.** | v3.0.0 release notes:52; v3.0.0 `linux_cgo.go:17`, `linux_cgo_gtk3.go:19`; GA template `updatable_build_assets/linux/nfpm/nfpm.yaml.tmpl:27-44` |
| 10 | macOS: `MacBackdropTransparent` leaves the webview opaque without `-tags private_mac_apis`; `Frameless` hides the traffic lights | server | `main.go:62-67`; own min/quit buttons at `frontend/src/components/Header.tsx:21-22` | **behaviour.** The transparent admin window renders opaque on macOS. Hidden traffic lights are fine. No macOS GUI is released (release builds headless only), so this only hits local builds. | **Q4.** | https://github.com/wailsapp/wails/pull/6087, https://github.com/wailsapp/wails/pull/5866, https://github.com/wailsapp/wails/pull/5870; v3.0.0 `webview_window_options.go:57-71,504-505` |
| 11 | Windows frameless hit-testing, dark-mode/menu, WebView2 recovery fixes | server | `main.go:63,70-72`, `build/windows/wails.exe.manifest` | **behaviour (fixes).** | None. Manual check (§9). | https://github.com/wailsapp/wails/pull/5462, https://github.com/wailsapp/wails/pull/5877, https://github.com/wailsapp/wails/pull/6002 |
| 12 | AppImage: Wails writes its own AppRun (no AppImageKit `continuous` download); runs linuxdeploy twice; bundles WebKit injected-bundle and `WebKitGPUProcess` | client | `.github/workflows/release.yml:154-159` (comment), `:160-166` (linuxdeploy pre-seed) | **packaging + user-visible fix.** The pre-seed filename `linuxdeploy-%s.AppImage` is unchanged (v3.0.0 `appimage.go:116-119`), so the pre-seed and the `buildcheck_test.go:456` pins keep working. Between the two linuxdeploy runs, GA binary-patches the bundled `libwebkit*gtk-*.so` (`relocateWebKitHelpers`) so WebKit spawns the helper processes shipped in the AppImage. Per the changelog, a beta.22 AppImage **crashed on start outside Debian/Ubuntu** ("Unable to spawn a new child process: … WebKitNetworkProcess"); GA fixes that. The "still unpinnable: AppImageKit's continuous AppRun" note (design R2) is stale. #6286 (`s.MOVE` falls back to copy on `EXDEV`) does not matter here: the release job's build dir and output dir are both inside the checkout. | Reword `release.yml:154-159` (§5.4). Manual check on a non-Debian distro (§9). | #6213 (`d8f3336`), #6267 (`8f8ef35`), #6286 (`94f9f15`, `internal/s/s.go` only; no changelog line, internal helper). Changelog line found: v3.0.0-beta.31 "Fixed" ("Fix AppImages from `wails3 generate appimage` crashing on start outside Debian and Ubuntu …"). v3.0.0 `appimage.go:116-124,164-179,203-231,244-347` |
| 13 | Production-tagged files changed: Windows `enableDevTools` returns `error` (into the WebView2 recovery path) instead of calling `handleFatalError`; tag expressions on `logger_dev.go`/`logger_prod.go` (add `server` mobile cases) and `webview_window_linux_dev.go` (`!production` → `(!production \|\| devtools)`) | client (server GUI too) | release builds `-tags production,purego` (`build/darwin/Taskfile.yml:54`, `build/linux/Taskfile.yml:54`, `build/windows/Taskfile.yml:74`) | **none.** Windows: `go build`/`go vet -tags production,purego` exit 0 (critic). Linux/darwin: by build-constraint analysis, `production,purego` without `devtools`/`server`/mobile selects the **same file set** at beta.22 and v3.0.0 (darwin constraints unchanged; the Linux change only matters with `devtools`, which we never set). Compiling them still needs the CI legs (cgo). | None expected; CI release legs confirm. | https://github.com/wailsapp/wails/pull/6222 ("finish WebView2 recovery error handling", changelog: "Improve WebView2 recovery error handling and resource cleanup on Windows"); v3.0.0 `webview_window_windows.go:2726` |
| 14 | Wails no longer requires `wailsapp/go-webview2` (WebView2 carried in-tree as `internal/webview2`) and drops go-git/go-billy/go-winloader etc. | server only (all three are already gone at the client's beta.22: beta.22 `.mod` has no go-webview2, and go-git only as indirect v5.19.2) | `go.mod` | **dependency** (§6.1) | None. | go-webview2: https://github.com/wailsapp/wails/pull/5317 then /5711 (changelog: "Fold the `webview2` binding into the v3 module as `v3/internal/webview2`"); go-git: /5520 ("replace go-git direct dependency with system git CLI"); go-winloader: /6031 (changelog: "drops … the `github.com/jchv/go-winloader` dependency") |
| 15 | Child-frame guard for `wails:` messages; `closeStarted` CompareAndSwap makes WindowClosing teardown idempotent; `InvokeSync` no longer blocks forever on a callback panic | client | `main.go:379-384` (WindowClosing hook + `Cancel`), `internal/app/app.go:272,301`, `windows_lock_test.go:13` | **none.** Hooks still run before listeners, so the close-to-tray `Cancel` at `main.go:381` still prevents teardown and never sets `closeStarted`. | None. | https://github.com/wailsapp/wails/pull/6288 / https://github.com/wailsapp/wails/pull/6344 (guard), https://github.com/wailsapp/wails/pull/6108; v3.0.0 `application.go:866-870`, `webview_window.go:366-374` |
| 16 | Every other Wails symbol either repo uses (`Event.Emit/EmitEvent`, `CustomEvent`, `BundledAssetFileServer`, `AssetFileServerFS`, `NewService`, windows, tray, menus, dialogs, `RegisterHook`, `App.Logger/Quit/Run`, `MacOptions`) | both | client: `main.go:103-118,122,133,363-401`, `internal/app/tray.go:27-37,52-73,90`, `windows_wails.go:21-52`, `emitter.go:13`, `app.go:65,70,185,272,301`, `profiles.go:549-552,585-588,604-608`, `history.go:275-278`, `tray_test.go:6`, `main_assets_test.go:14`; server: `main.go:12,30-82`, `app/app_gui.go:9,16,19,22,46,52,73,101,121` | **none.** `event_manager.go`, `events.go`, `services.go`, `window_manager.go`, `dialog_manager.go`, `system_tray_manager.go` and `pkg/events` are unchanged from beta.22 to v3.0.0. | None. | `go build`/`go vet` against v3.0.0 (both repos, both probes and verifier) |
| 17 | `internal/assetserver/asset_fileserver.go` byte-identical; still requires `index.html` | client | `main.go:28-29,106-107`, `main_assets_test.go:26,54,112`, `frontend/scripts/check-dist.mjs:17` | **none.** The guards stay relevant and pass. | None. | v3.0.0 `asset_fileserver.go:30`, `application_options.go:251` |
| 18 | `@wailsio/runtime` JS API (`Events.On`, `Window.Minimise`, `Application.Quit`) | both | client `frontend/src/shared/api/events.ts:1,59`, `TopBar.tsx:147,150`, `PreLoginTopBar.tsx:43,46`; client mocks `useSessionEndedNotice.test.tsx:9`, `History.test.tsx:43-44`, `Profiles.test.tsx:41-42`; server: 10 importing files (`ClientEntry.tsx:9`, `MessageWrapper.tsx:5`, `Header.tsx:7`, `ServerControl.tsx:7`, `BanManagement.tsx:5`, `ClientList.tsx:7`, `Coalitions.tsx:10`, `FrequencyPage.tsx:18`, `Logs.tsx:11`, `Settings.tsx:8`), server mocks `frontend/src/test/setup.ts:4,18` | **none.** npm beta.34 and 3.0.0 have identical `dist/` and `types/`. The server's deep import `@wailsio/runtime/types/events` (`ServerControl.tsx:8`, `Coalitions.tsx:13`, `FrequencyPage.tsx:22`, `Settings.tsx:9`) is not in the exports map at either version. It does **not** "work today" for tsc: it is 4 of the 11 non-test baseline errors (TS2307). It works only because `vite build` erases the type-only import and vitest aliases it (`vitest.config.ts:26-29` → `dist/events.js`, mocked at `src/test/setup.ts:18`). Unchanged by UP. | None. Vitest mocks unchanged. | `npm pack` diff (verifier); re-run tsc |
| 19 | Not used by either repo: `GlobalShortcut`, `KeyBindings`, `SingleInstance`, `LinuxOptions.ApplicationID`, `PromptForMultipleSelection`, question dialogs, `-tags mcp`/`server`, iframes, tray menu updates while open (#6301), macOS `WindowDidUpdate` | both | `git grep` finds only a comment at client `internal/hotkeys/session.go:22` | **none** | None. | https://github.com/wailsapp/wails/pull/6301; verifier grep |
| 20 | `wails3 dev` asset proxy forces `tcp4` for `localhost` (v3.0.0 `internal/assetserver/build_dev.go:92-94`; alpha.43 has no such code); `FRONTEND_DEVSERVER_URL` is `http://localhost:<port>` (`internal/commands/dev.go:24`) | server | `build/Taskfile.yml:74` `npm run dev -- --port {{.VITE_PORT}} --strictPort` (no `--host`); `frontend/vite.config.ts` sets no `server.host`. Client already passes `--host 127.0.0.1` (`build/Taskfile.yml:192`). | **behaviour (dev loop only, inferred from source, not reproduced).** If Vite binds `localhost` to `::1` only (common on Windows), the IPv4 dial is refused and `wails3 dev` serves nothing. | Append `--host 127.0.0.1` at `:74`, as the client does. UP introduces the risk, so UP fixes it; confirm with one local `wails3 dev` (§9). | build mapper item 9, build verifier (confirmed) |
| 21 | GA task wrapper runs the root `build`/`package` task and passes `GOOS=`/`ARCH=` as vars (alpha.43 ran `<GOOS>:build` directly); `task_wrapper.go` is byte-identical beta.22 → GA | server (client already has it) | server `Taskfile.yml:18,23` (and client `Taskfile.yml:48,53`) dispatch on `{{OS}}` | **behaviour, latent.** Native builds unchanged. `GOOS=windows wails3 build` on another host would now build for the host OS. Nothing in CI cross-builds with the CLI. | None in UP (optional later: `{{.GOOS \| default OS}}`). | alpha.43 `task_wrapper.go:58-65`; v3.0.0 `:35-38,142-153` |
| 22 | `wails3 dev`: a primary execute without `exit_policy` defaults to `shutdown` (v3.0.0 `internal/commands/watcher.go:27-33`, applied at `:64`) | both | client `build/config.yml:35` (`wails3 task run`, primary); server `build/config.yml:43` | **behaviour (dev only).** Closing the app window now ends the `wails3 dev` session. | None. | build mapper item 13, verifier (confirmed) |
| 23 | Template default for the Windows version-info language key changed `"0000"` → `"0409"` so Explorer's Details tab shows version info (#6210). `generate syso` is byte-identical and still accepts `"0000"`. | both | client `build/windows/info.json:6`, `internal/buildcheck/buildcheck_test.go:206`; server `build/windows/info.json:6` | **none (template improvement, not breakage).** Nothing UP does reads the template. | **Q6.** | changelog v3.0.0 line "Fix default Windows info.json language to en-US (0409) …"; beta.22 vs v3.0.0 `updatable_build_assets/windows/info.json.tmpl:6` |
| 24 | Wails v3 GitHub security advisories fixed at or before GA: GHSA-vfcr-9mmf-hc6m (macOS child frames), GHSA-c8qg-xhgr-798r (Windows notification PowerShell fallback), GHSA-vwx8-cwg4-xx23 (`wails3 init -t` argument injection), GHSA-r7hq-45w4-mfcx (server-mode `/wails/runtime` CSRF), all `< 3.0.0-beta.31`; GHSA-cv6g-2vcq-wcvx (server-mode WebSocket origin) `< 3.0.0-beta.8` | client (first four), server (all five) | none reachable: neither repo embeds iframes, uses Wails notifications (`pkg/services`), runs `wails3 init`, or builds with Wails' `-tags server` (the server's own `headless` tag is unrelated) | **security (Dependabot-visible, not govulncheck-visible).** UP closes them. | None. | `gh api repos/wailsapp/wails/security-advisories`; release notes:86 ("If you are on `v3.0.0-beta.30` or earlier, please upgrade") |

**Counts.** Client: **0** breaking changes (0 build, 0 typecheck, 0 test); 2 runtime behaviour changes, both fixes (#4, #5); 1 packaging change that is also a user-visible fix (#12); 4 advisories closed (#24). Server: **1** breaking change in the checked code (#1, 2 tsc errors from one cause) plus **1 local-only Linux GUI build break** (#9, never compiled here or in CI); 1 semantic binding change (#2); runtime/packaging/dev-loop behaviour changes #6–#8, #10, #11, #20–#22 (#8, #9, #10 need decisions; #20 gets a one-line fix).

**Citation corrections (verifier/critic/re-run), applied above:** the child-frame guard is #6288/#6344, not #6363 (a docs PR). #5898 is "Guard uninitialized GTK windows" (a nil-widget guard). The Linux opaque-background-before-load change is **#5899, now confirmed** (changelog: "Set explicit opaque background color for Linux WebKit windows before URL load"). It only applies to `BackgroundTypeSolid` windows with alpha 255, and it is already in the client's beta.22 (`webview_window_linux.go:394`). The server window is `BackgroundTypeTransparent` (`main.go:62`), so it takes the other branch and **#5899 has no effect on either repo**. #5552 is an issue, not a PR; the Linux mic/camera default is cited to v3.0.0 `linux_cgo.c:178-187`. B16-01 (#5919) is a macOS fix. The changelog lives at `docs/mpress/content/changelog.md` at tag v3.0.0 (moved from `docs/src/content/docs/changelog.mdx`). The client's production build flags are at `build/darwin/Taskfile.yml:54`, not `:57`.

## 4. Trial-bump results (verbatim from the probes)

### 4.1 Client (worktree of `e7c7c30`, removed)

| Command | Result | Output |
|---|---|---|
| `buf generate` | pass (baseline) | exit 0, no output |
| `go run …/wails3@v3.0.0-beta.22 generate bindings -ts -f "-tags purego" -clean=true` | pass (baseline) | `Wails v3.0.0-beta.22 › Generate Bindings. Processed: 414 Packages, 1 Service, 59 Methods, 1 Enum, 36 Models, 0 Events` |
| `npm ci && npm run build` | pass (baseline) | `found 0 vulnerabilities ... check-dist: dist/ contains index.html, main.html, comms.html, notifications.html` |
| `govulncheck@v1.9.0 -tags purego ./...` | pass (baseline) | `Your code is affected by 0 vulnerabilities.` 3 module-level: GO-2026-6866, GO-2026-6860 (x/net v0.60.0, fixed v0.62.0), GO-2026-5932 (x/crypto v0.58.0 openpgp, no fix) |
| `go get github.com/wailsapp/wails/v3@v3.0.0 && go mod tidy` | pass | `go: upgraded github.com/wailsapp/wails/v3 v3.0.0-beta.22 => v3.0.0`; tidy exit 0 |
| `npm install @wailsio/runtime@^3.0.0` | pass | `"^3.0.0-beta.34" -> "^3.0.0"`; lock resolves 3.0.0; `found 0 vulnerabilities` |
| `go run …/wails3@v3.0.0 generate bindings -ts -f "-tags purego" -clean=true` | pass | `Wails v3.0.0 › Generate Bindings. Processed: 416 Packages, 1 Service, 59 Methods, 1 Enum, 36 Models, 0 Events` |
| `diff -r bindings-before frontend/bindings` | one addition | `Only in frontend/bindings: events` |
| `go build -tags purego ./...` | pass | exit 0, no output |
| `go vet -tags purego ./...` | pass | exit 0, no output |
| `go test -race -tags purego ./...` | pass | 25 packages ok, 0 FAIL (`internal/buildcheck` included) |
| `go test -tags purego -run 'TestWindowPagesMatchSources\|TestViteBundleShapeIsServable\|TestEmbeddedAssetsServeWindowPages' -v .` | pass | `--- PASS x3` |
| `npx tsc --noEmit` | pass | no output |
| `npx vitest run` | pass | `Test Files 51 passed (51); Tests 594 passed (594)` |
| `npm run build` | pass | `built in 163ms; check-dist: dist/ contains index.html, main.html, comms.html, notifications.html` |
| `govulncheck@v1.9.0 -tags purego -show verbose ./...` | pass, same as baseline | `Your code is affected by 0 vulnerabilities.` Same 3 module-level findings |

Critic addition: `go build -tags production,purego .` and `go vet -tags production,purego . ./internal/app` exit 0 on a scratch copy (Windows only). Caveat: frontend exit codes were read through `| tail`; the verdicts rest on tool output. Only the Windows leg was exercised; Linux and macOS cgo builds could not run on this host.

### 4.2 Server (worktree of `09f9180`, removed)

| Command | Result | Output |
|---|---|---|
| 3× protoc + `go run github.com/99designs/gqlgen generate` (before) | pass | gen exit 0 (local libprotoc 36.0, CI pins 36.2; gqlgen changed only CRLF) |
| `go build` / `go vet -tags headless ./...` (before) | pass | `build headless 0 / vet headless 0` |
| `go build ./...` (GUI) with no `frontend/dist` (before) | FAIL, pre-existing | `main.go:15:12: pattern all:frontend/dist: no matching files found` |
| `npm run build` with no bindings (before) | FAIL, pre-existing | `Build failed with 14 errors: [UNRESOLVED_IMPORT] Could not resolve '../../bindings/…/controlservice'` |
| `wails3@v3.0.0-alpha.43 generate bindings -clean=true -ts` (before) | pass | `Processed: 640 Packages, 5 Services, 25 Methods, 0 Enums, 25 Models, 0 Events` |
| `npm run build; npx tsc --noEmit -p .; npm test`; GUI build/vet (before) | build pass; tsc FAIL 112 pre-existing; vitest pass; GUI pass | `tsc: 112 'error TS'` (e.g. `ServerControl.tsx(8,26): error TS2307: Cannot find module '@wailsio/runtime/types/events'`); `Test Files 16 passed, Tests 75 passed`; `build gui 0 / vet gui 0` |
| `go test -race -count=1 -tags headless ./...` (before) | pass | 10 packages ok |
| `go get …/wails/v3@v3.0.0 && go mod tidy` | pass | `upgraded wails/v3 v3.0.0-alpha.43 => v3.0.0; x/crypto v0.53.0=>v0.57.0; x/mod v0.37.0=>v0.41.0; x/net v0.56.0=>v0.60.0; …` |
| 3× protoc + gqlgen (after; gqlgen v0.17.89 in-module, x/tools v0.49.0) | pass | gen exit 0; graphql diff CRLF only; `tool github.com/99designs/gqlgen` kept |
| `go build` / `go vet -tags headless ./...`; `GOOS=linux CGO_ENABLED=0 go build -tags headless .` | pass | `build headless 0 / vet headless 0 / linux headless xbuild 0` |
| `wails3@v3.0.0 generate bindings -clean=true -ts`; diff | pass, shape changed | `Processed: 512 Packages, 5 Services, 25 Methods, 0 Enums, 24 Models`; maps `{ [_: string]: T }` → `{ [_ in string]?: T }`; `LastUpdate` `time$0.Time` → `string`; new `bindings/events/index.ts` |
| `npm install --legacy-peer-deps --save @wailsio/runtime@3.0.0` | pass | `^3.0.0-beta.34` → `^3.0.0`; `npm ls: @wailsio/runtime@3.0.0` |
| `npm run build; npx tsc --noEmit -p .; npm test` (after) | build pass; vitest pass; **tsc 114 (+2)** | `src/pages/ClientList.tsx(21,20): error TS2345: Argument of type '{ [x: string]: ClientState \| undefined; }' is not assignable to parameter of type 'SetStateAction<Record<string, ClientState> \| null>'.` and the same at `(39,24)`; `Test Files 16 passed, Tests 75 passed` |
| GUI `go build` / `go vet ./...` with rebuilt `frontend/dist` (after) | pass | `build gui 0 / vet gui 0` |
| `go test -race -count=1 -tags headless ./...` (after) | pass | 10 packages ok |
| govulncheck (after) | exit 3, fewer | both tag sets: `Your code is affected by 6 vulnerabilities from 2 modules` |

**tsc baseline (settled by the re-run).** Fresh `git archive` of server `09f9180` `frontend/`, `npm ci --legacy-peer-deps` (runtime 3.0.0-beta.34, TypeScript 7.0.2), the probe's saved alpha.43 and GA bindings, `npx tsc --noEmit`:

| Configuration | alpha.43 bindings | GA bindings | Δ |
|---|---|---|---|
| `-p .` (tsconfig `include: ["src"]`, so tests in `src/**/__tests__` and `src/test/` are included) | **112** | **114** | +2 (`ClientList.tsx(21,20)`, `(39,24)` TS2345; sorted diff shows nothing else) |
| tests excluded (`*.test.ts(x)`, `src/test/**`) | **11** | **13** | +2 (same two) |
| no bindings generated at all (`-p .` / tests excluded) | 140 / 39 | 140 / 39 | 0 |

The mapper's "19 → 21" is **not reproducible**: it is neither split. The likeliest cause is the stale local `node_modules` the build verifier found (runtime 3.0.0-beta.24 instead of the locked beta.34). **CI sees no count at all:** the server's `frontend-test` job (`test.yml:62-83`) runs `npm ci` and `npm test` (vitest) only, with no tsc step. It also generates no bindings, so a tsc step added to that job as it stands would report the third row (140 / 39) both before and after UP and would not catch #1. The 101 test-file errors (for example `toBeInTheDocument` missing on vitest's `Assertion`, and `../../bindings` paths resolved from `src/pages/__tests__/`) make a tsc gate impractical without a separate cleanup. This doc uses the `-p .` baseline (112 → 114 → back to 112 with the fix). The 11 non-test errors are 4 × the `@wailsio/runtime/types/events` deep import, a missing `AdminState` binding, and zod/MUI typing drift. All predate UP and are out of scope.

## 5. Plan shape

### 5.1 Client PR (`chore/cleanup-up`)

1. **RED: buildcheck pins (test first).** In `internal/buildcheck`, add two tests that fail on today's tree:
   - `TestWailsCLIVersionIsDerivedFromGoMod`: no file under `.github/workflows/` contains the regex `wails3@v[0-9]`, and every `wails3` install step uses the `go list -m` form (§5.3).
   - `TestWailsRuntimeMatchesGoModule`: the `@wailsio/runtime` spec in `frontend/package.json` equals the go.mod Wails version without the leading `v` (the pairing rule, §2), and `package-lock.json`'s `node_modules/@wailsio/runtime` version equals it too. Today it fails (`^3.0.0-beta.34` vs `v3.0.0-beta.22`).
2. **GATE (dependency):** `buf generate` first (D7), then `go get github.com/wailsapp/wails/v3@v3.0.0 && go mod tidy`; `npm install --save-exact @wailsio/runtime@3.0.0` (or `^3.0.0`, Q1). The second buildcheck test goes GREEN.
3. **GATE (ci-cd):** derive the CLI in `test.yml:324-325` and `release.yml:151-152` (§5.3; the release step also drives `wails3 task package` at `release.yml:173`); reword `release.yml:119-120` and `:154-159` (§5.4). **Q5 decides the go-deps part:**
   - Q5 = A (keep Wails excluded, recommended): change only the comment at `go-deps.yml:28` (to "Wails moves only with @wailsio/runtime; bump both by hand, see TestWailsRuntimeMatchesGoModule"). `TestGoDepsWorkflowShape` (`buildcheck_test.go:491`) keeps asserting the exclusion string.
   - Q5 = B (lift it): remove `GO_DEPS_EXCLUDE` and its comment at `go-deps.yml:28-29`, **and** update `TestGoDepsWorkflowShape` (`buildcheck_test.go:491`, comment at `:485` "Wails excluded"), which otherwise goes RED. Today's design missed this test.
   The first buildcheck test goes GREEN.
4. **Bindings regeneration:** `wails3 generate bindings -ts -f "-tags purego" -clean=true` with the derived CLI. Expected diff: only the new `frontend/bindings/events/index.ts` (gitignored, so nothing in the PR). Confirm `TestBoundSurfaceIsTheFrontendAPI` (`internal/app/surface_test.go:43`) is unchanged and green.
5. **Verification (no code to fix):** `go build`/`go vet`/`go test -race -tags purego ./...`, `go build -tags production,purego .`, the three `main_assets` tests, `tsc --noEmit`, `vitest run`, `npm run build` (check-dist), govulncheck. All CI legs (Windows, Linux, macOS) on the PR are the only cgo check for Linux/darwin.
6. **Manual-checklist additions** to `docs/superpowers/plans/2026-09-15-phase-3-manual-verification.md` (§9, client rows).

There is no behaviour code in the client, so the TDD obligation is met by the two buildcheck tests. The tray fixes (#4, #5) are upstream behaviour with no seam of ours to unit-test; they go to the manual checklist.

### 5.2 Server PR (`chore/cleanup-up`)

1. **RED (test first):** add a case to the **existing** `frontend/src/pages/__tests__/ClientList.test.tsx` (it already mocks `GetClients` with a two-client map) that feeds a `Clients` map with an extra `undefined` value and asserts the two defined entries render and nothing crashes. Today's `:47-48` passes `undefined` into `ClientEntry`, so the case fails. Note: Go cannot actually produce an `undefined` value here (value-type map, §3 #1); the test pins the type-level guard, not a reachable runtime bug. The existing mocks' `LastUpdate: null` become stale against #2 (vitest does not typecheck, so they still pass); optionally switch them to a string.
2. **GATE (dependency):** run codegen first (3× protoc + `go run github.com/99designs/gqlgen generate`, as `.github/actions/generate`), then `go get …/wails/v3@v3.0.0 && go mod tidy` (D7). `npm install --legacy-peer-deps --save-exact @wailsio/runtime@3.0.0` (Q1).
3. **Bindings regeneration** with the derived CLI (§5.3), then **fix `ClientList.tsx:11` and `:47`** as in §3 #1. GREEN: the new vitest passes and `npx tsc --noEmit -p .` returns to the identical 112-error set (re-run verified).
4. **Decisions Q2–Q4** applied if the user picks a non-default option (each is one line in `main.go` or `nfpm.yaml`, plus CI if Q2 says so).
5. **Server housekeeping that UP makes necessary:** `build/Taskfile.yml:74` gets `--host 127.0.0.1` (§3 #20); `.github/actions/generate/action.yml:20` "raises x/tools to v0.46.0" → v0.49.0, the version forced by Wails v3.0.0's own require (the "lowest that works" floor stays v0.46.0); `README.md:9-13` drops "Wails3 is currently in alpha" and the v3alpha.wails.io links, and installs the CLI with the derived command from §5.3 (`@latest` would now float to the newest weekly minor, not the go.mod version).
6. **GATE (ci-cd), per Q5:** A = reword the comment at `go-deps.yml:32` only; B = remove `GO_DEPS_EXCLUDE` at `:33` and its comment (the server has no buildcheck test on it).
7. **Verification:** protoc + gqlgen generate; headless build/vet/test `-race`; Linux headless cross-build; GUI build/vet with a built `frontend/dist`; vitest; govulncheck both tag sets.
8. **Manual checklist** rows (§9, server rows).

### 5.3 Deriving the wails3 CLI from go.mod (concrete mechanism)

Replace each literal install with the same pattern CL1 used for gqlgen and the repo uses for protoc-gen-go:

```yaml
- name: Install wails3 CLI (version from go.mod)
  shell: bash
  run: go install "github.com/wailsapp/wails/v3/cmd/wails3@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)"
```

- `go list -m` reads the main module's go.mod requirement, so the CLI and the library cannot drift. `shell: bash` is explicit because the release matrix includes Windows runners.
- **Not** an in-module `go install …/cmd/wails3` without `@version` and **not** a `tool` directive: both would pull the CLI's whole dependency graph into our go.mod/go.sum (a further dependency change) and make `go mod tidy -diff` (the Taskfile's CI tidy check) sensitive to CLI-only deps.
- Sites: client `.github/workflows/test.yml:325`, `.github/workflows/release.yml:152`. **Server: no workflow installs wails3** (server CI is headless-only). For the server the CLI is only used locally (`build/Taskfile.yml:55`, `Taskfile.yml:33`), and its install instruction lives in `README.md:13` (`wails3@latest`, reworded in §5.2 step 5). A GUI CI leg, if Q2 = B adds one, uses the same snippet.
- Pinned by `TestWailsCLIVersionIsDerivedFromGoMod` (§5.1 step 1).

### 5.4 `release.yml` comment rewording

- `:119-120`: "Wails v3 beta.22 does not link against" → "Wails v3 does not link against (GTK4/WebKitGTK 6.0 is the GA default)".
- `:154-159`: "(beta.22 internal/commands/appimage.go)" at `:155-156` → "(v3.0.0 internal/commands/appimage.go:116-119)"; drop "AppImageKit's `continuous` AppRun" (`:157-158`) from the still-unpinnable list (Wails v3.0.0 writes its own AppRun, `appimage.go:122`); keep "the AppImage runtime linuxdeploy fetches itself" and "Microsoft's WebView2 bootstrapper"; mark design R2 "re-checked in UP". Also update the same R2 list in `docs/superpowers/specs/2026-10-27-phase-6-9-cl5-build-ci-release-design.md:156`.

## 6. Gates needing approval

### 6.1 Dependency changes

**Client** (probe diff, saved as `upwf-go.mod.before` / `upwf-go.sum.before`):
- `go.mod:13` `github.com/wailsapp/wails/v3 v3.0.0-beta.22` → `v3.0.0`. No other require changes.
- `go.sum`: the two wails/v3 lines → `v3.0.0 h1:lBe89kFu23bVz1dc1H9v0srkHQgWsy7BAKpH4sLQuDw=` and `v3.0.0/go.mod h1:U3p3/QfSer2XxXM3XWhwis9O7LvvuNKN/AcGoEFD+K8=`. **No transitive changes** (MVS keeps our x/net and x/crypto).
- `frontend/package.json:17` `"^3.0.0-beta.34"` → `"3.0.0"` (Q1; probe used `^3.0.0`).
- `package-lock.json`: 4 lines; `node_modules/@wailsio/runtime` → 3.0.0, integrity `sha512-NkFJXYWD/4Y3TcHAqfIeST4oybtwv9dVWTmriZTMFbINYiQ2FB3Wr4MjsQ7o0BNynEj2GslmUisNejnixEmLVQ==`.

**Server** (probe diff, saved as `upwf-server-gomod.diff`, `upwf-server-frontend.diff`):
- Direct: `wailsapp/wails/v3 v3.0.0-alpha.43` → `v3.0.0`; `golang.org/x/crypto v0.53.0` → `v0.57.0`; `golang.org/x/mod v0.37.0` → `v0.41.0`.
- Unchanged: grpc v1.77.0, protobuf v1.36.11, gqlgen v0.17.89, gqlparser v2.5.32, quic-go v0.54.0, qpack v0.5.1. The `tool github.com/99designs/gqlgen` directive is kept.
- Indirect upgraded: x/net v0.56.0 → v0.60.0, x/sys v0.46.0 → v0.48.0, x/text v0.38.0 → v0.42.0, **x/tools v0.46.0 → v0.49.0** (a direct Wails require, so UP forces it; still above the gqlgen/Go 1.27 floor of v0.46.0, and gqlgen generate passes), x/sync v0.21.0 → v0.23.0, godbus/dbus/v5 v5.1.0 → v5.2.2.
- Indirect removed (28): dario.cat/mergo, Microsoft/go-winio, ProtonMail/go-crypto, bep/debounce, cloudflare/circl, cyphar/filepath-securejoin, ebitengine/purego, emirpasic/gods, go-git/gcfg, go-git/go-billy/v5, go-git/go-git/v5, golang/groupcache, jbenet/go-context, jchv/go-winloader, kevinburke/ssh_config, leaanthony/go-ansi-parser, leaanthony/u, lmittmann/tint, pjbgf/sha1cd, pkg/browser, rivo/uniseg, sergi/go-diff, skeema/knownhosts, wailsapp/go-webview2, wailsapp/mimetype, xanzy/ssh-agent, gopkg.in/ini.v1, gopkg.in/warnings.v0.
- Indirect added: coder/websocket v1.8.14 (via wails `pkg/application`); kr/pretty v0.3.1, rogpeppe/go-internal v1.14.1, gopkg.in/check.v1 (yaml.v3 test deps pulled in by tidy).
- `frontend/package.json` `@wailsio/runtime` `^3.0.0-beta.34` → `3.0.0` (Q1); `package-lock.json` 6+/5-.

### 6.2 CI changes

| Repo | File:line | Change |
|---|---|---|
| client | `.github/workflows/test.yml:324-325` | literal `wails3@v3.0.0-beta.22` → derived (§5.3) |
| client | `.github/workflows/release.yml:151-152` | same |
| client | `.github/workflows/release.yml:119-120`, `:154-159` | comment rewording (§5.4) |
| client | `.github/workflows/go-deps.yml:28-29` | Q5 = A: reword the comment only. Q5 = B: remove the Wails `GO_DEPS_EXCLUDE` and its comment, **plus** `internal/buildcheck/buildcheck_test.go:485,491` (`TestGoDepsWorkflowShape`) |
| client | `internal/buildcheck/` (new tests) | `TestWailsCLIVersionIsDerivedFromGoMod`, `TestWailsRuntimeMatchesGoModule` (§5.1) |
| client | `.github/dependabot.yml:7-14` (only if Q5 = B) | `ignore: @wailsio/runtime` in the npm entry, so the runtime moves only with go.mod (§10 Q5) |
| server | `.github/workflows/go-deps.yml:32-33` | Q5 = A: reword the comment. Q5 = B: remove the Wails `GO_DEPS_EXCLUDE` and its comment |
| server | `.github/actions/generate/action.yml:20` | comment only: x/tools figure (§5.2 step 5) |
| server | (new, only if Q2 = B) | a Linux GUI build job |

No change to protoc/buf/gqlgen pins, to the client's pinned server checkout (`test.yml` checks out the server and builds it headless, which does not link Wails: only server `main.go:1` and `app/app_gui.go:1` import it, both `//go:build !headless`), or to the Dockerfile (`-tags production,headless`).

### 6.3 Cross-repo order

- **Independent.** No wire contract changes (Wails is not on the wire). Client CI builds the pinned server headless, which does not link Wails, so a client bump never needs a server bump and vice versa.
- **One forward-compat check:** when the client's server pin later moves past the server's UP merge, the client CI's in-module `go run github.com/99designs/gqlgen generate` runs under x/tools v0.49.0. The server probe ran exactly that and it passed.
- **Recommendation: two separate PRs, client first.** The client PR has zero source changes and an empty generated diff, so it is the low-risk proof of the CLI-derivation pattern. The server PR then carries the typecheck fix and the Q2–Q4 decisions. Each repo's `go-deps.yml` change (per Q5) lands in that repo's PR.

## 7. Security effect (for SEC)

**Client: no change.** govulncheck v1.9.0 `-tags purego` before and after the bump is identical: **0 reachable**, 0 in imported packages, 3 module-level and unreachable: GO-2026-6866 and GO-2026-6860 (`golang.org/x/net@v0.60.0`, fixed v0.62.0) and GO-2026-5932 (`golang.org/x/crypto@v0.58.0` openpgp, no fix). UP neither adds nor removes a finding. x/net v0.62.0 belongs to the weekly Go-deps track / SEC.

**Server: 21 → 6 (GUI) and 10 → 6 (headless).** govulncheck v1.9.0 on `09f9180`:
- headless before: 10 in 3 modules: x/net v0.56.0 (GO-2026-6617, -6612, -6611, -6603), grpc v1.77.0 (GO-2026-6443, -6348, -6061, -4762), quic-go v0.54.0 (GO-2026-5676, GO-2025-4233).
- GUI before: 21 in 7 modules: the above plus x/net GO-2026-6610, go-git v5.13.2 (GO-2026-6214, -5496, -5105, -4910, -4909, -4473), go-billy v5.6.2 (GO-2026-5597, -5490), x/text v0.38.0 (GO-2026-5970), circl v1.6.1 (GO-2026-4550). All of the GUI-only ones come in through Wails alpha.43.
- **after (both tag sets): 6 in 2 modules**: grpc v1.77.0 (GO-2026-6443, -6348, -6061, -4762) and quic-go v0.54.0 (GO-2026-5676, GO-2025-4233).

**What it means for SEC:**
- UP clears all 5 x/net findings (via the indirect x/net v0.56.0 → v0.60.0 move) and all 10 GUI-only go-git/go-billy/x/text/circl findings (tidy drops or upgrades them).
- **SEC still owns grpc** (needs ≥ v1.83.1 to clear all four: fixes at 1.79.3, 1.82.1, 1.82.2, 1.83.1) **and quic-go** (≥ v0.59.1). UP does not move either.
- **Overlap:** UP moves server x/net to v0.60.0. If SEC bumps x/net (for example to v0.62.0, which also clears GO-2026-6866/6860 that the client sees at v0.60.0), whichever track merges second must rebase go.mod/go.sum; MVS takes the higher version, so there is no conflict in substance, only in the diff. Recommend SEC treats server x/net as "≥ v0.60.0 after UP" and does not duplicate the bump.
- Lifting the Wails `GO_DEPS_EXCLUDE` (Q5 = B) would put Wails back under the weekly update, so future Wails security releases would not be silently skipped. Q5 = A keeps that a manual job. The recommendation relies on the Wails advisories feed (Dependabot alerts still fire on go.mod even though Dependabot does not update Go here).
- **Wails' own advisories (§3 #24).** govulncheck shows none of them, but GitHub lists five v3 advisories fixed at or before GA. The client's beta.22 is inside four (`< 3.0.0-beta.31`) and the server's alpha.43 inside all five. None is reachable in our usage (no iframes, no Wails notifications service, no Wails `server` tag, no `wails3 init`), but Dependabot alerts on them close with UP.

## 8. Risks

- **R1 GA is two days old.** A `v3.0.1` may follow quickly; the 3.1 nightly already exists. Mitigation: re-run §4 on any 3.0.x before implementation; never take a nightly.
- **R2 Linux and macOS cgo never compiled here.** Neither the GTK4 default `linux_cgo.go` nor the darwin `.m` paths (including `private_mac_apis` gating and server `main.go:66`), nor the Linux/darwin `production` builds, ran on this Windows host. The client's CI legs cover Linux and macOS; **the server GUI is compiled by no CI job at all**, so its Linux/macOS GUI paths stay unverified unless Q2 adds a leg.
- **R3 Server typecheck is invisible to CI.** Server CI runs only `npm test` (`test.yml:62-83`); vitest mocks the bindings and `vite build` does not typecheck. The ClientList fix is guarded only by the new vitest and by a local `tsc` count. A future binding-shape change will slip through the same way.
- **R4 Packaging untested.** No `wails3 task package` ran for NSIS (template byte-identical beta.22 → GA; makensis 3.13.0 at `release.yml:135,173`), AppImage (new AppRun, double linuxdeploy, WebKitGPUProcess bundling) or the macOS `.app` (`ditto` zip at `release.yml:217`, arm64). Only the release workflow on a tag will show them.
- **R5 Server Linux packaging mismatch** (#9) until Q2 is decided.
- **R6 x/net overlap with SEC** (§7).
- **R7 Template drift (audited by the re-run; see appendix A.3).** Not regenerating is safe for UP in both repos: every `wails3` subcommand and flag the Taskfiles call still exists, and `wails3 build`/`package` go through a wrapper that is byte-identical beta.22 → GA. Regenerating is **not** safe and stays out of UP: `wails3 update build-assets` rewrites the client's guarded customisations and recreates `build/linux/nfpm/`, which CL5 deleted on purpose.
- **R8 Evidence gaps (mostly closed by the re-run).** Closed with PR and changelog evidence: the AppImage AppRun change (#6213/#6267, beta.31 changelog line), go-webview2 leaving the requires (#5317/#5711), the darwin Taskfile changes (#6225/#6237), the `enableDevTools` signature change (#6222), and the Linux opaque background (#5899). #6286 has a PR but no changelog line, and it is irrelevant to our builds. Still open: GitHub release bodies for beta.2–beta.20 were not read (the changelog was used), and attribution in the server's alpha.44–59 range relies on the alpha.78-tag changelog copy and is weak.
- **R9 Incomplete mapping. Resolved:** the completeness re-run covered all three areas (appendix).
- **R10 Generated code provenance.** The verifier's scratch builds used generated proto/graphql code copied from local working trees, not a fresh codegen of `origin/main`. The probes did run fresh codegen and agree.
- **R11 Weekly Wails minors vs. the exact pin (new, drives Q5).** From GA on, Wails ships a **minor every week** (release notes:107). If the go-deps exclusion is lifted, the client's weekly go-deps run would `go get wails/v3@upgrade` (to v3.1.0, …) without touching npm. `go-deps.yml:114` runs `go test ./...`, which includes `internal/buildcheck`, so `TestWailsRuntimeMatchesGoModule` fails, the `update` job fails, and **no go-deps PR opens for any module that week**. The server has no such test, but its go-deps run and its CI are headless-only, so a weekly Wails minor (v3.1 drops `-tags gtk3`, and binding output can change as in #1) would merge with zero compile coverage of the GUI.

## 9. Manual verification (real hardware, webview or packaging only)

**Client** (add to `docs/superpowers/plans/2026-09-15-phase-3-manual-verification.md`, which is itself still unrun):

> Note (added when this design was committed): this section is kept as the approved text. Some of its line references are stale (for example `tray.go:52-58`, `windows_wails.go:39-52`). The authoritative, corrected references are in that checklist's §14.

1. Windows: right-click the tray icon → menu opens (it never did on beta.22). Repeat with the icon in the overflow (`^`) flyout. Show VCS and Settings work (`tray.go:52-58`). **Quit** runs `ServiceShutdown` (`app.go:301`) and the hotkey stream closes cleanly (no hang, no race with the signal handler).
2. Linux: right-click opens the menu **only**; the main window does not toggle. Left-click still toggles (`tray.go:37,62-73`). The tooltip still does nothing on Linux at 3.0.0 (expected).
3. Close-to-tray: closing the main window hides it, not quits (`main.go:379-384`).
4. Multi-window: main, comms and notifications open, focus, resize, move and restore their saved geometry (`windows_wails.go:39-52`).
5. Release artefacts from a tagged run: the Windows NSIS installer installs and launches; the Linux AppImage launches **on a non-Debian distro (for example Fedora or Arch) as well as on Ubuntu**, which is exactly what GA fixes (the new AppRun, the relocated WebKit helpers, the bundled `WebKitGPUProcess`); the macOS `.app` zip launches on arm64.

**Server GUI** (local builds only; no CI builds it):
6. Linux: the GUI builds and launches under the chosen GTK (Q2); a second launch behaves as Q3 decides. (#5899's "no flash before load" does not apply: the server window is transparent.)
7. macOS: the admin window renders as Q4 decides (opaque by default); no traffic lights; the header's own minimise/quit work.
8. Windows: drag by the masked region, behaviour across mixed-DPI monitors, no click-through on the transparent frameless window.
9. Ctrl-C / SIGTERM to the GUI prints "Quitting application..." and exits.
10. The Logs page receives `logs/entry` in order under load.
11. Dev loop, Windows: the `wails3 dev` task (server `Taskfile.yml:33`) serves the admin UI with the `--host 127.0.0.1` change (§3 #20). Closing the window now also ends the dev session (§3 #22).

## 10. Questions for the user

**Q1 – npm pin form.** The v3.0.0 release notes say to pin `@wailsio/runtime` exactly.
**Decided by the user (2026-10-28): A.**
- **A (recommended):** exact `"3.0.0"` in both `package.json` files, enforced by `TestWailsRuntimeMatchesGoModule` in the client. The Wails Go version and the runtime then move together, by hand (Q5 = A) or by a combined go-deps + npm bump (Q5 = C). Q1 = A with Q5 = B is the combination that breaks the weekly go-deps run (R11).
- B: `"^3.0.0"` (what the probe did). Looser; a later 3.x runtime could pair with a 3.0.0 Go host.

**Q2 – server Linux GUI toolkit (#9).** (Unchanged, with sharper facts from the re-run: the GA default also **breaks the local Linux GUI build** on a GTK3-only dev host, and the current nfpm deps `gtk3` / `libwebkit2gtk` are already not valid Debian package names, so the package was broken before UP.)
**Decided by the user (2026-10-28): A.**
- **A (recommended):** follow the GA default (GTK4 / WebKitGTK 6.0): change `build/linux/nfpm/nfpm.yaml:27-29` to the GA template's names: deb `libgtk-4-1`, `libwebkitgtk-6.0-4`, plus the template's `overrides` for rpm (`gtk4`, `webkitgtk6.0`) and archlinux (`gtk4`, `webkitgtk-6.0`) (v3.0.0 `nfpm.yaml.tmpl:27-44`). Developers need `libgtk-4-dev` / `libwebkitgtk-6.0-dev`, matching the client. No new CI job in UP; record "server GUI never built in CI" as a follow-up.
- B: as A, and add a Linux GUI build job to server CI (frontend build + bindings + `go build`), using the §5.3 CLI snippet. Catches R2/R3 for the server, but is new CI scope.
- C: pin `-tags gtk3` and keep the current packaging. Only works until Wails v3.1 drops it.

**Q3 – server second instance on Linux (#8).**
**Decided by the user (2026-10-28): A.**
- **A (recommended):** accept the new behaviour. A second instance fails on port bind with an error. No code change, nothing new adopted.
- B: set `Options.SingleInstance` in `main.go` to restore one instance per machine. Behaviour-preserving, but it adds an option.

**Q4 – server macOS transparent backdrop (#10).**
**Decided by the user (2026-10-28): A.**
- **A (recommended):** accept an opaque window. No macOS GUI is released, and `private_mac_apis` is a private-API / App Store risk.
- B: add `-tags private_mac_apis` to the server's macOS GUI build to keep the transparency.
- C: change `MacBackdropTransparent` to Normal and drop the transparency intent explicitly.

**Q5 – Wails in the weekly go-deps bump (new in the re-run; the build mapper flagged it as a user decision, the first draft decided it silently).** Both `go-deps.yml` files say the exclusion lasts "until UP lands". From GA on, Wails releases a minor every week, and the `@wailsio/runtime` npm package must match it exactly (R11).
**Decided by the user (2026-10-28): A.**
- **A (recommended):** keep `GO_DEPS_EXCLUDE: github.com/wailsapp/wails/v3` in both repos and reword the comment: Wails is bumped by hand, Go and npm together, with the §4 probe re-run each time. This is compatible with Q1 = A and the pairing test; `TestGoDepsWorkflowShape` stays as is.
- B: lift it in both repos. Then the client also needs a Dependabot npm `ignore` for `@wailsio/runtime` (`.github/dependabot.yml:7-14`) and a way to bump npm together with go.mod. Without that, every week with a new Wails minor fails the whole go-deps run on `TestWailsRuntimeMatchesGoModule` (R11). It also requires the `buildcheck_test.go:491` edit and accepts weekly Wails minors into the server with no GUI compile check.
- C: lift it, and teach `go-deps-update.sh`/`go-deps.yml` to bump `@wailsio/runtime` in the same PR. This is the most automatic option and new CI scope beyond UP.

**Q6 – Windows version-info language key `0000` → `0409` (#23; new in the re-run, flagged by the build mapper as a user decision and dropped by the first draft).**
**Decided by the user (2026-10-28): B.**
- **A (recommended):** leave it out of UP. It is a GA *template* fix, not upgrade breakage (`generate syso` is unchanged), and adopting it is a hand edit. Record it as a follow-up for the packaging track.
- B: adopt it in UP: client `build/windows/info.json:6` plus `buildcheck_test.go:206`; server `build/windows/info.json:6`. Explorer's Details tab would then show the version info of the shipped `.exe`.

## Appendix: Completeness review (re-run)

The first critic received the area mapping cut at 40,000 characters and never saw `build-packaging-ci-deps` or the platform/cgo items. This re-run read the following in full:
- all 3 mappings: `app-window-tray-events` (15 items), `bindings-runtime-frontend-assets` (12) and `build-packaging-ci-deps` (19);
- their 3 verifier passes;
- both probes, the first critic and the design writer.

It re-checked locations against `git show` of client `e7c7c30` and server `09f9180`. It closed the thin-evidence items from the module cache (`wails/v3@v3.0.0`, `@v3.0.0-beta.22`, `@v3.0.0-alpha.43`) and from `gh` (PRs, commits, advisories, and the changelog at tag v3.0.0). Nothing in either repo or their worktrees was modified. Scratch work is in `scratchpad/rerun/`.

### A.1 Every mapped item and how the design handles it

**build-packaging-ci-deps** (never critiqued before):

| Item | Verifier | Handling in this design |
|---|---|---|
| 0 CLI pin derivation (GA-22, B8-02, S-a2-103) | confirmed | §5.3. **Added:** server `README.md:13` (`wails3@latest`) to §5.2 step 5. |
| 1 client go.mod (GA-01/02, S-go-min …) | confirmed | §6.1 (no change needed). |
| 2 server go.mod (S-a2-114, B6-04 …) | confirmed + correction + 2 missed | §6.1. **Added:** the `action.yml:20` comment is definitely stale (D3, §5.2 step 5); tidy must run after codegen (D7, §5.1/§5.2 step 2). |
| 3 govulncheck | confirmed | §7. The mapper's client run used govulncheck v1.1.4 and the probe used v1.9.0; both show the same 3 unreachable findings. |
| 4 GO_DEPS_EXCLUDE (GA-22, **needs decision**) | confirmed | **The first draft decided this silently ("lift").** Now **Q5**, with R11 (the conflict with the pairing test) and the `buildcheck_test.go:491` edit the first draft missed. |
| 5 npm exact pin (GA-25, …) | missing-locations | Q1. The server deep-import locations are now in §3 #18, and the tsc claim there is corrected. The Dependabot npm `ignore` is in Q5 = B. |
| 6 AppImage (GA-18) | missing-locations | §3 #12, §5.4. **Added:** `release.yml:155`, the CL5 design's R2 line (`:156`), and the `buildcheck_test.go:456` pins (unchanged). |
| 7 info.json 0409 (GA-19, **needs decision**) | confirmed | **The first draft dropped this.** Now §3 #23 and **Q6**. |
| 8 darwin template (GA-20) | confirmed | D6, §1. **Corrected:** this is a fix (#6225/#6237) for output folders synced by a macOS FileProvider (iCloud), not a new feature. It stays out. |
| 9 server dev proxy tcp4 (GA-20, S-a89, S-a79) | confirmed (inferred) | **The first draft dropped this.** Now §3 #20, a one-line fix in §5.2 step 5, and manual check §9.11. |
| 10 server GTK4 default (S-a93, GA-21), **breaks-build** | confirmed | §3 #9 reclassed to include the local Linux GUI build break; Q2 sharpened. |
| 11 task-wrapper GOOS dispatch (S-a2-111) | confirmed | **The first draft dropped this.** Now §3 #21 (latent, no action). |
| 12 template-only changes (S-a44-prod-default …) | confirmed | A.3. These are reached only if templates are regenerated, and UP does not regenerate them. |
| 13 `exit_policy` default | confirmed | **The first draft dropped this.** Now §3 #22 and §9.11. |
| 14 CLI subcommand/flag surface | confirmed | R7 / A.3: every invocation is still valid. |
| 15 client bindings diff | confirmed | §3 #3. |
| 16 client `build:server` tasks (GA-23) | confirmed | No action. A later cleanup outside UP can delete them. |
| 17 client CI's pinned server SHA | confirmed | §6.3 (the repos are independent). |
| 18 no-touch-point entries | confirmed | No action. |

**app-window-tray-events** (platform behaviour; critiqued before, re-checked):
- Items 0–14 map to §3 #4–#8, #10, #11, #15, #16 and #19.
- Verifier corrections applied:
  - the missing call-site locations (§3 #16);
  - the overflow-flyout fallback (§3 #4, §9.1);
  - the mailbox wording (§3 #6);
  - the guard citation is #6288/#6344;
  - B16-01 is a macOS fix.
- **Re-run correction for item 9:** #5899 is confirmed as the opaque-background PR, but it has no effect on either repo (§3, citation paragraph). The mapper's "a Linux GUI build of the server should be added to CI" is option Q2 = B; the design does not assume it.

**bindings-runtime-frontend-assets** (critiqued before, re-checked):
- Verifier corrections applied:
  - npm `dist/` identity;
  - the check-dist line (`:15-16`);
  - mimetype is at `go.mod:82`;
  - in-module `go install` is rejected (§5.3).
- **Re-run correction:** the ClientList guard is at `:47` (`Object.entries`), and the value is used at `:48`.
- The verifier called the fix "optional hygiene". It stays in the plan because it costs two lines and is the only way to keep the local tsc count from regressing.

### A.2 Thin-evidence items

| Item | Result | Evidence |
|---|---|---|
| AppImage AppRun change | **closed** | #6213 (`d8f3336`, edits `UNRELEASED_CHANGELOG.md`) and #6267. The changelog line is under v3.0.0-beta.31 "Fixed": AppImages crashing outside Debian/Ubuntu. The module diff of `appimage.go` from beta.22 to GA shows: the AppImageKit download is removed; AppRun is written inline; linuxdeploy runs twice, and `relocateWebKitHelpers` binary-patches the WebKit library between the runs; `WebKitGPUProcess` is bundled. |
| #6286 | **closed** (no changelog line exists) | PR "fix(v3/build): move AppImage to output dir across filesystems" (`94f9f15`) touches only `internal/s/s.go` and its test. `s.MOVE` now falls back to a copy on `EXDEV`. This does not matter for our release, where the build and output dirs are on one filesystem inside the checkout. |
| go-webview2 leaving the requires | **closed** | #5317 migrated it into the monorepo, and #5711 folded it into `v3/internal/webview2` (changelog line). go-git left in #5520 and go-winloader in #6031. The `.mod` files show all of them gone by beta.22, so this affects the server only. |
| darwin Taskfile signing / `prepare-macos-output` | **closed** | #6225 "keep macOS app output outside file providers" and #6237 "resolve macOS GA readiness regressions", both with changelog lines. They fix codesign failures when output sits in an iCloud/FileProvider-synced folder. Out of scope (D6). |
| production-tag `enableDevTools` signature | **closed** | #6222 "finish WebView2 recovery error handling and cleanup" (changelog: "Improve WebView2 recovery error handling…"). The error now feeds `setupChromiumConfigError` (v3.0.0 `webview_window_windows.go:2726`) instead of being fatal during recovery. Comparing build constraints shows the same file set for `production,purego` on all three OSes. |
| #5899 (Linux opaque background) | **closed** | Confirmed: `c1db8e7`, with a changelog line in the beta.5 era. It is already in beta.22, and it applies only to Solid windows with alpha 255. The server window is Transparent, so the change has no effect. |

### A.3 Template drift (checked-in `build/` vs v3.0.0 templates)

`wails3 update build-assets` (client `build/Taskfile.yml:213`, server `build/Taskfile.yml:80`) extracts **only** `updatable_build_assets/`:
- darwin `Info.plist` and `Info.dev.plist`;
- linux `desktop` and `nfpm/nfpm.yaml`;
- windows `info.json`, `nsis/wails_tools.nsh` and `wails.exe.manifest`.

No command regenerates Taskfiles, `config.yml`, `project.nsi` or icons. Those come from `build_assets/` at `wails3 init` only, so "regenerating" them means porting by hand.

**Client** (beta.22 templates, customised by CL5). Between beta.22 and GA, three templates changed: `config.yml`, `darwin/Taskfile.yml` and `windows/info.json.tmpl`. The iOS/Android templates also changed but are unused. The NSIS templates are byte-identical.
- *If we do not regenerate (the plan), nothing breaks:*
  - `info.json` stays `"0000"` (Q6);
  - the #6225 darwin steps stay absent, which only matters for local macOS builds in synced folders;
  - `config.yml` needs no change (#22).
- *If we regenerate:*
  - It recreates `build/linux/nfpm/nfpm.yaml`, which CL5 removed on purpose. `TestNoUnsupportedPackagingTasks` scans only Taskfiles, so it would not catch the file.
  - The manifest's hand-set `version="0.1.0.0"` is replaced by `{{.ProductVersion}}`, which would likely fail `buildcheck_test.go:226/259`.
  - `Info.plist` is backed up and re-extracted. Whether `NSMicrophoneUsageDescription` (guarded by `TestInfoPlistKeepsMicrophoneUsage`) survives is unverified.
  - `wails_tools.nsh` gains `WAILS_INSTALL_SCOPE`. The default is `machine`, so behaviour does not change.
- **Do not regenerate in UP.**

**Server.** Its `build/` was scaffolded around May 2025, before alpha.43, and is heavily customised: each platform Taskfile differs from either template set by 166–224 lines. The updatable files still hold template placeholders, such as "My Company", "My Product", and `vcs-server.exe` as the Linux binary name.
- *If we do not regenerate (the plan):*
  - Every `wails3` invocation is still valid.
  - The `PRODUCTION` variable keeps its meaning, because the GA wrapper injects only GOOS, ARCH and tags.
  - #21 (GOOS dispatch) stays latent.
  - Two edits are still needed: #20 (`--host`) and Q2 (the nfpm deps).
  - Server CI uses nothing under `build/`.
- *If we regenerate the updatable files:*
  - nfpm gets the GTK4 deps (Q2 = A) and also re-templated paths;
  - `info.json` gets `0409` (Q6 = B) but keeps the placeholders unless `config.yml`'s `info` is filled in;
  - the manifest gains `trustInfo asInvoker`;
  - `wails_tools.nsh` gains the install scope;
  - `Info.plist`'s minimum macOS goes from 10.13 to 12.0 (GA's minimum, #5233).
- The risk is low, but it is unreviewed churn in product metadata, so it is **not in UP**. Q2 = A is a hand edit of `nfpm.yaml:27-29` only.

### A.4 Mapped as breaking, but not hit by the probes

| Change | Why the probes did not hit it |
|---|---|
| S-a93 GTK4 default (server Linux GUI build) | It is Linux cgo. The probes ran on Windows, and server CI and go-deps build with `-tags headless`, which does not link Wails. |
| S-a44 Taskfile templates default to production | It reaches a repo only through template regeneration (A.3). The probes ran the CLI directly. |
| S-a46 `linux:WindowLoadChanged` removed, S-a48 package-level dialog funcs removed, S-a56 file-drop renames, S-a66 WebView2 flags moved, S-a84 fullscreen tri-state | These are compile-level API removals that neither repo uses, and the GA compile passed. The `events.Linux.*` constants are not OS-tagged, so a Windows build would have caught any use. |
| S-a91 macOS coordinate space, B19-01 `private_mac_apis` | These affect macOS at runtime only and have no compile effect (§3 #10, Q4). |
| S-a2-111 task-wrapper GOOS dispatch | It shows only with `GOOS=… wails3 build/package`, which nobody runs (§3 #21). |
| GA-20 dev proxy tcp4 | It shows only in `wails3 dev`, and the probes did not run the dev loop (§3 #20). |
| S-a68 map keys | **Hit** by the probe (tsc +2; §3 #1). Listed for completeness. |

### A.5 Server tsc baseline

Settled in §4.2:
- 112 → 114 with tests (`-p .`);
- 11 → 13 without tests;
- 140 / 39 with no bindings generated.

The mapper's 19 → 21 cannot be reproduced. CI runs no tsc, so it sees none of these counts. With the §3 #1 fix applied, GA returns to the identical 112-error set.

### A.6 Questions

- **Q1–Q4 still stand.** Q2 keeps its options and recommendation, with sharper facts: the local build breaks, the existing deps are already invalid, and the GA template gives the exact package names.
- **Q1 now interacts with Q5.** Choosing Q1 = A together with Q5 = B breaks the weekly go-deps run.
- **Q5 (Wails in go-deps) and Q6 (info.json `0409`) are new.** The build mapper flagged both `needs_user_decision`. The first draft decided Q5 without asking and dropped Q6.

### A.7 Not closed

- No Linux or macOS cgo compile has run in either repo, including the production tag sets and the server GUI. No packaging run has happened either (NSIS, AppImage, `.app`). These still need the client's CI legs and a tagged release run. The server GUI has no CI at all; Q2 = B would add some.
- §3 #20 (dev proxy on `::1`) is inferred from source and has not been reproduced.
- How `Info.plist` merges on regeneration (A.3) was not exercised.
- The GitHub release bodies for beta.2–beta.20 were not read, and attribution in the server's alpha.44–59 range remains weak (R8).
