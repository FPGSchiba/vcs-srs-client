package app

import (
	"context"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

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

// WailsApp returns the injected Wails app (for main.go window/event wiring).
func (a *App) WailsApp() *application.App { return a.wailsApp }

// ServiceStartup is the Wails v3 lifecycle hook.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.logger.Info("App service starting up")
	return nil
}

// ServiceShutdown is the Wails v3 lifecycle hook.
func (a *App) ServiceShutdown() error {
	a.logger.Info("App service shutting down")
	return nil
}
