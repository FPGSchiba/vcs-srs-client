package app

import (
	"context"
	"fmt"

	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App is the main application service. All exported methods are callable from
// the frontend via Wails3 generated bindings.
type App struct {
	logger   *slog.Logger
	wailsApp *application.App
}

// NewApp creates a new App service. Call SetApp after creating the
// *application.App so this service can create additional windows.
func NewApp(logger *slog.Logger) *App {
	return &App{logger: logger}
}

// SetApp injects the Wails application reference. Must be called before Run().
func (a *App) SetApp(app *application.App) {
	a.wailsApp = app
}

// ServiceStartup is called by Wails3 when the service starts.
// Replaces the v2 OnStartup(ctx context.Context) lifecycle hook.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.logger.Info("App service starting up")
	return nil
}

// ServiceShutdown is called by Wails3 when the app is closing.
func (a *App) ServiceShutdown() error {
	a.logger.Info("App service shutting down")
	return nil
}

// Greet returns a greeting for the given name.
// Callable from the frontend via generated bindings.
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// OpenNewWindow creates a new WebviewWindow with the given title.
// Callable from the frontend via generated bindings.
func (a *App) OpenNewWindow(title string) {
	if a.wailsApp == nil {
		a.logger.Error("OpenNewWindow called before SetApp — wailsApp is nil")
		return
	}
	a.wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  title,
		Width:  800,
		Height: 600,
		URL:    "/",
	})
}
