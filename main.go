package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/pkg/logger"
)

//go:embed all:frontend/dist
var assets embed.FS

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
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	gui.SetApp(wailsApp)

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "VCS Client",
		Width:            1280,
		Height:           720,
		URL:              "/",
		BackgroundColour: application.NewRGBA(27, 38, 54, 255),
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
