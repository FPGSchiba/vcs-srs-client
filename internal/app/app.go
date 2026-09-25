package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// keybindsRawFromConfig converts cfg.Keybinds's persisted-value type into the
// plain-string-slice shape keybinds.Store.Load expects. internal/config knows
// nothing about internal/trigger's grammar and internal/keybinds knows
// nothing about internal/config's on-disk shape, so this conversion has to
// live at the layer that imports both.
func keybindsRawFromConfig(src map[string]config.KeybindValue) map[string][]string {
	out := make(map[string][]string, len(src))
	for id, v := range src {
		out[id] = []string(v)
	}
	return out
}

// sessionAPI is the session surface the bindings depend on (fakeable in tests).
type sessionAPI interface {
	Connect(ctx context.Context, serverURL, name, password, unitID string) error
	Disconnect(ctx context.Context) error
	Reconnect(ctx context.Context) error
	UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error
}

// windowsAPI is the window-registry surface the bindings depend on.
type windowsAPI interface {
	Open(id string)
	Close(id string)
	Toggle(id string)
	OpenWindows() []string
	Geometry(id string) windowstate.Geometry
	SetGeometry(id string, g windowstate.Geometry)
}

// App is the Wails service and the frontend binding surface.
type App struct {
	logger   *slog.Logger
	wailsApp *application.App
	st       *state.Store
	sess     sessionAPI
	windows  windowsAPI
	settings *settingsBackend
	tray     *application.SystemTray

	// perm is the OS seam for the global-hotkey capture permission (macOS
	// Accessibility). Filled in by SetSettingsBackend with the platform
	// implementation unless a test has already injected a fake through
	// setPermissionChecker. Written once, before anything reads it.
	perm hotkeys.PermissionChecker

	// voice is the App-level voice wiring: the live session, the refcounted
	// TX set and the Sink/Source bridge main.go registers with the audio
	// Manager exactly once, at startup. See voice.go.
	voice voiceState
}

// NewApp creates the App with its logger. Backend wiring happens in SetBackend.
func NewApp(logger *slog.Logger) *App {
	a := &App{logger: logger, st: state.New()}
	a.initVoice()
	return a
}

// NewForTest builds an App with injected fakes (no Wails app).
func NewForTest(st *state.Store, sess sessionAPI, windows windowsAPI) *App {
	a := &App{logger: slog.Default(), st: st, sess: sess, windows: windows}
	a.initVoice()
	return a
}

// Store exposes the state store for wiring in main.go.
func (a *App) Store() *state.Store { return a.st }

// SetBackend injects the live session + registry (called from main after SetApp).
func (a *App) SetBackend(sess sessionAPI, windows windowsAPI) {
	a.sess = sess
	a.windows = windows
}

// SetApp injects the Wails application reference. Must be called before Run().
func (a *App) SetApp(app *application.App) { a.wailsApp = app }

// SetSettingsBackend wires the settings/keybind dependencies onto App and
// seeds kb from cfg.Keybinds, falling back to keybinds.Defaults() when
// cfg.Keybinds is empty (fresh config, or one written before Phase 3).
func (a *App) SetSettingsBackend(cfg *config.Config, cfgPath string, kb *keybinds.Store, hk *hotkeys.Manager, em events.Emitter) {
	src := cfg.Keybinds
	if len(src) == 0 {
		src = defaultKeybindsRaw()
	}
	kb.Load(keybindsRawFromConfig(src))

	if a.perm == nil {
		a.perm = hotkeys.NewPermissionChecker()
	}

	deviceNames := make(map[string]string, len(cfg.KeybindDevices))
	for id, name := range cfg.KeybindDevices {
		deviceNames[id] = name
	}

	a.settings = &settingsBackend{
		cfg:            cfg,
		cfgPath:        cfgPath,
		kb:             kb,
		hk:             hk,
		em:             events.New(em),
		captureTimeout: defaultCaptureTimeout,
		permInterval:   defaultPermissionPollInterval,
		permTimeout:    defaultPermissionPollTimeout,
		presses:        newPressCount(),
		holds:          map[string]bool{},
		deviceNames:    deviceNames,
	}
	// Per-radio actions are derived from the local client's radios, which are
	// empty at this point and only arrive at connect time. Observe the store
	// so the keybind list and the OS registrations follow them (I2); without
	// it the per-radio panel stays absent for the whole session unless the
	// user happens to touch an unrelated keybind.
	a.st.OnRadiosChanged(a.RefreshKeybinds)
	// Seeds the initial OS registration AND emits the first hotkeys:state, so
	// a startup registration failure (no backend, denied permission) reaches
	// the UI without waiting for the user to change something. applyHotkeys
	// reads the permission on its way through, which also seeds lastPerm for
	// RecheckHotkeyPermission's early-out.
	a.applyHotkeys()
}

// setPermissionChecker injects a fake PermissionChecker. For tests only, and
// only before SetSettingsBackend -- that is what makes the field a
// write-once value no goroutine can race.
func (a *App) setPermissionChecker(p hotkeys.PermissionChecker) { a.perm = p }

// defaultKeybindsRaw renders keybinds.Defaults() in cfg.Keybinds's persisted
// shape, for the case where no config.toml keybinds exist yet (fresh config,
// or one written before Phase 3).
func defaultKeybindsRaw() map[string]config.KeybindValue {
	defaults := keybinds.Defaults()
	out := make(map[string]config.KeybindValue, len(defaults))
	for id, list := range defaults {
		strs := make([]string, 0, len(list))
		for _, t := range list {
			strs = append(strs, t.String())
		}
		out[string(id)] = config.KeybindValue(strs)
	}
	return out
}

// WailsApp returns the injected Wails app (for main.go window/event wiring).
func (a *App) WailsApp() *application.App { return a.wailsApp }

// ServiceStartup is the Wails v3 lifecycle hook.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.logger.Info("App service starting up")
	return nil
}

// shutdownDisconnectTimeout bounds the clean-leave RPC on quit. Without it an
// unresponsive server would hang the quit forever on a context.Background()
// call the user cannot cancel.
const shutdownDisconnectTimeout = 2 * time.Second

// shutdownPollStopTimeout bounds how long quit waits for an armed
// hotkey-permission re-check to return. Short: the goroutine returns
// immediately on cancel unless it is mid-apply or waiting on writeMu.
const shutdownPollStopTimeout = time.Second

// ServiceShutdown is the Wails v3 lifecycle hook. It runs the clean
// disconnect, so the server sees a proper leave rather than a dropped stream
// on EVERY quit path -- tray Quit, Cmd+Q, and closing the window with
// minimize_to_tray off (the ordinary quit on Windows and Linux). Doing this
// in the tray's Quit handler alone covered exactly one of those.
func (a *App) ServiceShutdown() error {
	a.logger.Info("App service shutting down")
	// Stop any armed hotkey-permission re-check first. Quitting within 30s of
	// clicking GRANT ACCESS would otherwise leave a goroutine free to call
	// into the hotkey library and emit a Wails event after teardown. Bounded,
	// so a poll blocked behind an in-flight keybind write cannot hang quit.
	a.stopPermissionPoll(shutdownPollStopTimeout)
	// Then shut the OS key listener down. The stream is process-global and
	// outlives every rebind (see internal/hotkeys/registrar_gohook.go), so
	// this is its one closing bracket. It also releases a hotkey still being
	// HELD at quit: someone hitting Cmd+Q mid-transmission would otherwise
	// have Pressed emitted with no Released to match it.
	if a.settings != nil && a.settings.hk != nil {
		a.settings.hk.Close()
	}
	// Stop any live voice session before the control disconnect, mirroring
	// App.Disconnect's ordering -- ServiceShutdown is a SEPARATE quit path
	// (tray Quit, Cmd+Q, closing the window with minimize-to-tray off) and
	// does not go through the Disconnect binding, so it needs its own call
	// or a voice session outlives the control connection it was dialed
	// against, sending BYE nowhere and leaking its goroutines past quit.
	a.stopVoiceSession()
	if a.sess != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownDisconnectTimeout)
		defer cancel()
		if err := a.sess.Disconnect(ctx); err != nil {
			a.logger.Warn("clean disconnect on shutdown failed", "err", err)
		}
	}
	return nil
}
