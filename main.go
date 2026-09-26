package main

import (
	"embed"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
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

	// Connection health: one model, one ticker, one event, fed from the same
	// call sites that emit the control lifecycle event (see
	// session.Deps.OnControlState) so the two cannot disagree about the link.
	healthEvents := vcsevents.New(emitter)
	interval := time.Duration(cfg.PingIntervalSeconds) * time.Second
	monitor := connhealth.New(connhealth.Options{
		Interval: interval, // 0 falls back to connhealth.DefaultInterval
		Ping:     sess.PingOnce,
		VoiceRTT: gui.VoiceRTT,
		// The Monitor does not set disconnected itself: the session emits it
		// through its single normal path, which comes straight back in via
		// SetControlState. One emission path, two detectors.
		OnLoss: sess.MarkControlLost,
		OnChange: func(s connhealth.Snapshot) {
			healthEvents.ConnectionHealth(app.ConnectionStateDTOFrom(s))
		},
	})
	// This is only the PRE-LOGIN seed: cfg.ServerURL is never written by
	// Settings or the Welcome form, so it is "" on every default install.
	// App.Connect calls monitor.SetServer again with the address actually
	// dialed the moment a connection succeeds (F1 fix, Phase 6 whole-branch
	// review) -- this call exists purely so a pre-connect window (or a
	// future persisted-server-URL feature) has something honest to show.
	monitor.SetServer(cfg.ServerURL)
	gui.SetConnHealth(monitor)
	monitor.Start()
	defer monitor.Stop()

	// session.New's construction above is unchanged. The observer is
	// installed after the monitor exists, because the two halves need each
	// other -- the monitor probes through sess.PingOnce, the session reports
	// through the monitor's SetControlState -- so one of the two links must
	// be late-bound, and the session is the one with somewhere to put it.
	//
	// sfxGate dedupes the SFX side of this observer (F4 fix, Phase 6
	// whole-branch review). monitor.SetControlState already dedupes against
	// the state it already holds, but gui.PlayConnectionSFX does not, and the
	// two calls are not the same question: a known, accepted duplicate
	// `disconnected` -- Disconnect firing after the probe detector has
	// already declared loss -- reaches this closure twice with the state
	// unchanged both times. Silent today because connect.wav/disconnect.wav
	// do not exist; would otherwise double-play the instant they land. Do
	// NOT fix this by changing the double-emit itself: it is deliberately
	// deferred (see the session/connhealth docs), and every OTHER consumer
	// of this state is already idempotent.
	sfxGate := &sfxDedup{}
	sess.SetControlStateObserver(func(st vcsevents.ConnectionState) {
		monitor.SetControlState(string(st))
		if sfxGate.shouldPlay(string(st)) {
			gui.PlayConnectionSFX(string(st))
		}
	})

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
					Inputs:  app.AudioDeviceDTOs(inputs),
					Outputs: app.AudioDeviceDTOs(outputs),
				})
			},
			OnState: func(st audio.State) {
				// One shared converter with GetAudioState (see
				// app.AudioStateDTOFrom): a hand-written mapping here is
				// what silently dropped the device/substitution fields
				// from the event path while State carried them.
				audioEvents.AudioState(app.AudioStateDTOFrom(st))
			},
			OnVU: func(v audio.VU) {
				audioEvents.AudioVU(audioVUPayload{Input: v.Input, Output: v.Output})
			},
		})
		// The voice Sink/Source bridge, registered exactly ONCE, Manager-
		// lifetime (design decision D5 -- Phase 4's Manager has no
		// RemoveSink and never calls Sink.Close(), so a per-connection
		// registration would leak a Sink on every reconnect). gui.voice.go
		// owns pointing it at the live *voice.Session on connect/disconnect.
		//
		// BOTH calls are required. AddSink alone wires TRANSMIT only --
		// receive comes back through the SEPARATE SetSource call, and
		// forgetting it leaves every existing test green while received
		// audio is silently discarded (see voice.go's package doc; Task 9's
		// own RX tests exercise internal/voice directly, not through this
		// Manager, so nothing there would catch a missing SetSource either).
		am.AddSink(gui.VoiceBridge())
		am.SetSource(gui.VoiceBridge())
		// SetAudioBackend BEFORE Start, not after: it is what pushes the
		// user's PERSISTED audio settings (device ids included) into the
		// manager. Starting first would resolve both devices against
		// NewManager's built-in default Config, whose InputDevice/
		// OutputDevice are "" -- i.e. the saved device selection would be
		// ignored on every single launch, and only take effect after the
		// poll loop's next tick noticed the mismatch. Configure, then start.
		gui.SetAudioBackend(am)
		if err := am.Start(); err != nil {
			appLog.Warn("audio engine failed to start; audio features are disabled", "err", err)
		}
		// Backend ownership lives HERE, with the code that constructed it --
		// Manager.Stop() deliberately no longer closes it (see Stop's doc:
		// Close is terminal, so a Stop/Start cycle would have nil-deref'd
		// the malgo context). Registered before the am.Stop() defer so that
		// LIFO order runs Stop first and Close second.
		defer backend.Close()
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

// The device-list and state converters both live in internal/app
// (AudioDeviceDTOs / AudioStateDTOFrom) rather than being duplicated here.
// The duplicate this replaced is precisely how the "System Default" sentinel
// could have reached GetAudioDevices' consumers while the hot-plug event
// path kept handing out lists without it.

// audioVUPayload is the audio:vu event's wire shape. audio.VU itself carries
// no json tags (it is an internal engine type, not a wire DTO), so its
// exported Go field names ("Input"/"Output") would leak onto the wire
// verbatim without this -- and Task 16's frontend store is specified to read
// lowercase `input`/`output`.
type audioVUPayload struct {
	Input  float32 `json:"input"`
	Output float32 `json:"output"`
}

// sfxDedup skips a repeated identical control-state transition, so a
// consumer with no dedup logic of its own (gui.PlayConnectionSFX) does not
// double-fire on a known, accepted duplicate emission of the same state (see
// its call site's doc, F4 in the Phase 6 whole-branch review).
//
// The zero value's last field is "", which every real events.ConnectionState
// string value ("connected"/"reconnecting"/"disconnected") differs from, so
// the very first transition always plays -- no separate "nothing played yet"
// flag is needed.
type sfxDedup struct {
	mu   sync.Mutex
	last string
}

// shouldPlay reports whether state differs from the last state this gate
// let through, and records state as the new last-played value either way --
// including when it returns false, so a THIRD repeat is still suppressed.
func (d *sfxDedup) shouldPlay(state string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state == d.last {
		return false
	}
	d.last = state
	return true
}
