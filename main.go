package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/pkg/logger"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	appLog := logger.CreateLogger()

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
