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
	// Input Monitoring). Filled in by SetSettingsBackend with the platform
	// implementation unless a test has already injected a fake through
	// setPermissionChecker. Written once, before anything reads it.
	perm hotkeys.PermissionChecker
}

// NewApp creates the App with its logger. Backend wiring happens in SetBackend.
func NewApp(logger *slog.Logger) *App {
	return &App{logger: logger, st: state.New()}
}

// NewForTest builds an App with injected fakes (no Wails app).
func NewForTest(st *state.Store, sess sessionAPI, windows windowsAPI) *App {
	return &App{logger: slog.Default(), st: st, sess: sess, windows: windows}
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
	raw := cfg.Keybinds
	if len(raw) == 0 {
		raw = defaultKeybindsRaw()
	}
	kb.Load(raw)

	if a.perm == nil {
		a.perm = hotkeys.NewPermissionChecker()
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

// defaultKeybindsRaw renders keybinds.Defaults() as the raw string map
// keybinds.Store.Load expects.
func defaultKeybindsRaw() map[string]string {
	defaults := keybinds.Defaults()
	out := make(map[string]string, len(defaults))
	for id, c := range defaults {
		out[string(id)] = c.String()
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

// ServiceShutdown is the Wails v3 lifecycle hook. It runs the clean
// disconnect, so the server sees a proper leave rather than a dropped stream
// on EVERY quit path -- tray Quit, Cmd+Q, and closing the window with
// minimize_to_tray off (the ordinary quit on Windows and Linux). Doing this
// in the tray's Quit handler alone covered exactly one of those.
func (a *App) ServiceShutdown() error {
	a.logger.Info("App service shutting down")
	if a.sess != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownDisconnectTimeout)
		defer cancel()
		if err := a.sess.Disconnect(ctx); err != nil {
			a.logger.Warn("clean disconnect on shutdown failed", "err", err)
		}
	}
	return nil
}
