package main

import (
	"embed"
	"log"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	vcsevents "github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/joystick"
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

	appLog, closeLog := logger.New(logger.Options{
		Level:    logger.ParseLevel(cfg.LogLevel),
		JSON:     true,
		FilePath: logPath, // empty if resolution failed → logger uses its default
	})
	defer closeLog.Close() //nolint:errcheck // process is exiting either way; nothing left to report to

	// Install appLog as the slog default so packages that log through
	// slog.Default() reach the rotating FILE, not just stderr.
	//
	// This is load-bearing, not tidiness. internal/keybinds/store.go destroys
	// a binding when a config lists more than one keyboard chord for an
	// action, and justifies that destruction on the grounds that the drop is
	// diagnosable -- it emits a Warn naming the action and the dropped chord.
	// Nothing ever called slog.SetDefault in production, so that Warn went to
	// stderr alone, and a Wails GUI build on Windows has no console: the
	// warning was discarded outright while its unit test passed, because the
	// test installs a handler of its own. Same for internal/app/app.go's
	// slog.Default() fallback and the Windows joystick backend's.
	//
	// appLog is logger.New's handler over io.MultiWriter(stderr, lumberjack),
	// so this adds a route, it does not duplicate one: nothing else bridges
	// slog.Default() to appLog. It also redirects the standard log package's
	// output here, which is what carries the log.Fatal at the bottom of main
	// into the log file instead of a console nobody sees.
	//
	// SetLogLoggerLevel FIRST, and it is not optional. SetDefault routes the
	// standard log package through a handlerWriter pinned at LevelInfo, which
	// DROPS the record when the handler is not enabled at that level. So with
	// log_level = "WARN" or "ERROR" -- a perfectly ordinary setting for a user
	// cutting noise -- log.Fatal(err) at the bottom of main would write
	// NOWHERE: not the file, not stderr, where before this line it at least
	// reached stderr. The app would exit 1 in total silence on the single most
	// important message it can emit. Error is enabled at every level ParseLevel
	// can return, so pinning it there keeps that message alive whatever the
	// user configured.
	slog.SetLogLoggerLevel(slog.LevelError)
	slog.SetDefault(appLog)

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

	// Joystick/gamepad input. A failure here is never fatal: the client is a
	// voice-comms app first, and keyboard binds must keep working on a
	// machine with no joystick, no permission to read one, or no backend at
	// all (macOS). The manager reports "unsupported" and the UI hides the
	// affordance.
	if joySrc, err := joystick.NewOSSource(appLog); err != nil {
		appLog.Warn("joystick input unavailable; keyboard binds are unaffected", "err", err)
	} else {
		jm := joystick.New(joySrc, gui, appLog)
		gui.SetJoystickBackend(jm)
		defer jm.Close()
	}

	// Audio engine. Exactly the same optional-dependency discipline as
	// joystick input just above: NewMalgoBackend can fail for reasons that
	// have nothing to do with whether the rest of the app should run -- no
	// sound card, a denied OS permission, a broken driver -- and none of
	// them may stop the user from connecting, seeing the UI, or using
	// keybinds and joystick input. internal/app's audio bindings are all
	// documented no-ops (or ErrAudioUnavailable) when SetAudioBackend is
	// never called, so a nil manager here is a fully supported path, not a
	// degraded one.
	//
	// Unlike the joystick manager, a failed construction here leaves nothing
	// behind that can ever emit audio:state on its own -- there is no
	// Manager -- so this pushes one audio:state event by hand, carrying the
	// error, so the frontend gets an honest answer instead of silence
	// indistinguishable from "audio hasn't started reporting yet".
	audioEvents := vcsevents.New(emitter)
	if backend, err := audio.NewMalgoBackend(); err != nil {
		appLog.Warn("audio backend unavailable; audio features are disabled", "err", err)
		audioEvents.AudioState(app.AudioStateDTO{
			InputError:  err.Error(),
			OutputError: err.Error(),
		})
	} else {
		am := audio.NewManager(backend, audio.ManagerOptions{
			Log: appLog,
			// OnDevices/OnState/OnVU are handed straight to the typed
			// emitter: events.Tagged.Emit forwards to the Wails
			// EventManager, whose EventProcessor.Emit only reads a
			// (locked) listener map and hands the payload to an internal
			// mailbox -- safe to call from more than one goroutine at
			// once, which is exactly what OnState/OnVU's doc comments say
			// Manager will do (Start's tail call and the poll/DSP
			// goroutines can all reach these concurrently).
			OnDevices: func(inputs, outputs []audio.DeviceInfo) {
				audioEvents.AudioDevicesChanged(app.AudioDevicesDTO{
					Inputs:  audioDeviceDTOs(inputs),
					Outputs: audioDeviceDTOs(outputs),
				})
			},
			OnState: func(st audio.State) {
				audioEvents.AudioState(app.AudioStateDTO{
					Running:     st.Running,
					InputError:  st.InputError,
					OutputError: st.OutputError,
					Overruns:    st.Overruns,
					Underruns:   st.Underruns,
				})
			},
			OnVU: func(v audio.VU) {
				audioEvents.AudioVU(audioVUPayload{Input: v.Input, Output: v.Output})
			},
		})
		if err := am.Start(); err != nil {
			appLog.Warn("audio engine failed to start; audio features are disabled", "err", err)
		}
		gui.SetAudioBackend(am)
		defer am.Stop()
	}

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
	// regains focus. On macOS, granting Accessibility means leaving the
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

// audioDeviceDTOs converts the engine's device list into the wire-facing
// shape, mirroring internal/app's own (unexported) audioDeviceDTOs used by
// GetAudioDevices -- kept as a small duplicate here rather than exporting
// that one, since main.go's use is a one-off event payload, not a binding.
func audioDeviceDTOs(devs []audio.DeviceInfo) []app.AudioDeviceDTO {
	out := make([]app.AudioDeviceDTO, 0, len(devs))
	for _, d := range devs {
		out = append(out, app.AudioDeviceDTO{ID: d.ID, Name: d.Name, IsDefault: d.IsDefault})
	}
	return out
}

// audioVUPayload is the audio:vu event's wire shape. audio.VU itself carries
// no json tags (it is an internal engine type, not a wire DTO), so its
// exported Go field names ("Input"/"Output") would leak onto the wire
// verbatim without this -- and Task 16's frontend store is specified to read
// lowercase `input`/`output`.
type audioVUPayload struct {
	Input  float32 `json:"input"`
	Output float32 `json:"output"`
}
