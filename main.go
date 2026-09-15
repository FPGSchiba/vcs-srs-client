package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/version"
	"github.com/FPGSchiba/vcs-srs-client/pkg/logger"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/trayicon.png
var trayIcon []byte

func main() {
	// Resolve the rotating log path under the OS app-data dir. On failure, fall
	// back to the logger's default relative path so startup still proceeds.
	logPath, logPathErr := config.LogFilePath()

	// Load the config, creating config.toml with defaults on first run so the
	// user has a file to edit. Fall back to in-memory defaults on any error.
	cfg := config.Default()
	cfgPath, cfgPathErr := config.ConfigFilePath()
	var cfgLoadErr error
	if cfgPathErr == nil {
		if loaded, err := config.LoadOrCreate(cfgPath); err == nil {
			cfg = loaded
		} else {
			cfgLoadErr = err
		}
	}

	appLog := logger.New(logger.Options{
		Level:    logger.ParseLevel(cfg.LogLevel),
		JSON:     true,
		FilePath: logPath, // empty if resolution failed → logger uses its default
	})

	if logPathErr != nil {
		appLog.Warn("could not resolve app-data log path; using default location", "err", logPathErr)
	}
	if cfgPathErr != nil {
		appLog.Warn("could not resolve config path; using in-memory defaults", "err", cfgPathErr)
	}
	if cfgLoadErr != nil {
		appLog.Warn("could not load or create config; using in-memory defaults", "err", cfgLoadErr)
	}

	gui := app.NewApp(appLog)

	wailsApp := application.New(application.Options{
		Name:        "VCS Client",
		Description: "VCS SRS Client",
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Services: []application.Service{
			application.NewService(gui),
		},
		Mac: application.MacOptions{
			// Must be false when close-to-tray is on, or hiding the last window
			// quits the app -- the exact opposite of the intent.
			ApplicationShouldTerminateAfterLastWindowClosed: !cfg.General.MinimizeToTray,
		},
	})

	gui.SetApp(wailsApp)

	// Wire backend: emitter → session → window registry → bindings.
	emitter := app.NewWailsEmitter(wailsApp)
	sess := session.New(gui.Store(), emitter, session.Deps{Version: version.Client})

	winPath, winPathErr := config.WindowStateFilePath()
	if winPathErr != nil {
		appLog.Warn("could not resolve windows.json path; geometry will not persist", "err", winPathErr)
		winPath = "windows.json"
	}
	registry := app.NewRegistry(app.NewWailsFactory(wailsApp), winPath, emitter)
	gui.SetBackend(sess, registry)

	// Keybind store, seeded from config (falls back to shipped defaults on a
	// fresh install), and the OS hotkey manager. App itself implements
	// hotkeys.Handler, so it receives Pressed/Released directly.
	kb := keybinds.New()
	hk := hotkeys.New(hotkeys.NewOSRegistrar(), gui)
	gui.SetSettingsBackend(cfg, cfgPath, kb, hk, emitter)

	// Main window: frameless + transparent, fixed 1440x900, loads the main entry.
	// Named so the tray (internal/app/tray.go) can resolve it back out of the
	// Wails window manager for show/hide.
	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             app.MainWindowName,
		Title:            "VCS Client",
		Width:            1440,
		Height:           900,
		URL:              "/main.html",
		Frameless:        true,
		DisableResize:    true, // main client is a fixed 1440x900 surface per the design
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGBA(3, 7, 13, 255), // --bg-0, opaque (no click-through)
	})

	gui.SetupTray(trayIcon)

	// Hide instead of close when close-to-tray applies; OnMainWindowClose
	// also force-disables this if the tray failed to come up (spec R13).
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if gui.OnMainWindowClose() {
			e.Cancel()
			mainWindow.Hide()
		}
	})

	// Re-check the OS global-hotkey permission whenever the main window
	// regains focus. On macOS, granting Input Monitoring means leaving the
	// app for System Settings and coming back, and the OS offers no
	// notification for the change -- so returning focus is both the moment
	// the answer can have changed and the cheapest time to look. All the
	// policy (is a re-check even warranted, did it flip, re-apply and emit)
	// lives in RecheckHotkeyPermission so this stays pure wiring.
	mainWindow.RegisterHook(events.Common.WindowFocus, func(_ *application.WindowEvent) {
		gui.RecheckHotkeyPermission()
	})

	if cfg.General.StartMinimized {
		mainWindow.Hide()
	}

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
