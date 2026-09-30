package app

import (
	"os"
	"strings"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/google/uuid"
)

// setHistory wires the transmission log. A nil log leaves every binding and
// both sources inert, the same discipline setNotifier documents.
func (a *App) setHistory(l *history.Log) { a.hist = l }

// SetHistory wires the transmission log. Package-level for the reason
// SetNotifier documents: nothing package-level is bound by
// application.NewService, so main.go keeps its call and the renderer gains
// nothing.
func SetHistory(a *App, l *history.Log) { a.setHistory(l) }

// HistoryWired reports whether a transmission log is attached. Exists so
// main's wiring test can assert the log reached App: an unwired log makes
// every history binding early-return, leaving the Transmission Log
// permanently empty with nothing logged to explain it.
func HistoryWired(a *App) bool { return a.hist != nil }

// StartHistoryTicker drives the log's debounced flush from a 1s ticker.
// Package-level, like SetHistory, so it is not bound into the webview.
//
// The goroutine is owned by App so ServiceShutdown can stop AND JOIN it
// before the final Flush: history.Flush writes through one fixed temp path
// (path + ".tmp"), so a tick-driven flush racing the shutdown flush would
// interleave on that file. A second call, or a call with no log, is a no-op.
func StartHistoryTicker(a *App) {
	if a.hist == nil || a.histStop != nil {
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	a.histStop, a.histDone = stop, done
	log := a.hist
	go func() {
		defer close(done)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				log.Tick()
			}
		}
	}()
}

// stopHistoryTicker stops the ticker goroutine and waits for it to exit, so
// no tick-driven Flush is in flight afterwards. Idempotent.
func (a *App) stopHistoryTicker() {
	if a.histStop == nil {
		return
	}
	close(a.histStop)
	<-a.histDone
	a.histStop, a.histDone = nil, nil
}

// flushHistory stops the ticker, THEN writes the log one last time. The
// order is the point; see StartHistoryTicker.
func (a *App) flushHistory() error {
	a.stopHistoryTicker()
	if a.hist == nil {
		return nil
	}
	return a.hist.Flush()
}

// resolveSender maps a voice sender id to the store's GUID key and the
// client's callsign.
//
// It does NOT look the store up by sender.String(). The store is keyed by
// the SERVER's GUID string, and uuid.Parse -- which App.voiceDialInputs
// already uses on that same string -- accepts uppercase, brace-wrapped and
// unhyphenated forms that UUID.String() does not round-trip to. A direct
// string lookup would therefore miss silently and render a blank sender,
// with nothing logged to explain it. Parsing every key and comparing UUID
// VALUES makes the spelling irrelevant.
//
// Returns ("", "") when the client is not in the store, which is normal:
// they may have left before the transmission was detected as over.
func (a *App) resolveSender(id uuid.UUID) (string, string) {
	if a.st == nil {
		return "", ""
	}
	for guid, info := range a.st.Snapshot().Clients {
		parsed, err := uuid.Parse(guid)
		if err != nil || parsed != id {
			continue
		}
		return guid, info.GetName()
	}
	return "", ""
}

// channelNameFor returns the LOCAL radio name tuned to freq, or "".
//
// An empty result is a real answer, not a failure: server global channels
// are accepted without a tuned radio (voice.rxContext.global), so there is
// no local name to show, and inventing one would be a lie. The UI renders
// the frequency alone.
func (a *App) channelNameFor(freq voice.KHz) string {
	sb := a.settings
	if sb == nil {
		return ""
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for _, r := range sb.cfg.Radios {
		if voice.KHz(r.FrequencyKHz) == freq {
			return r.Name
		}
	}
	return ""
}

// appendHistory records one entry and broadcasts it.
func (a *App) appendHistory(e history.Entry) {
	if a.hist == nil {
		return
	}
	a.hist.Append(e)
	if sb := a.settings; sb != nil && sb.em != nil {
		sb.em.HistoryAppended(historyEntryDTO(e))
	}
}

// onVoiceRX records one received transmission. Wired to voice.Options.OnRX,
// so it runs on that session's dedicated delivery goroutine -- never on the
// decode goroutine.
func (a *App) onVoiceRX(ev voice.RXEvent) {
	guid, callsign := a.resolveSender(ev.Sender)
	a.appendHistory(history.Entry{
		At:         ev.Start.UTC(),
		Sender:     callsign,
		GUID:       guid,
		FreqKHz:    uint32(ev.Freq),
		Radio:      a.channelNameFor(ev.Freq),
		DurationMS: int(ev.End.Sub(ev.Start) / time.Millisecond),
	})
}

// selfCallsign returns the local client's callsign, or "".
//
// Self is a *srspb.ClientInfo and is nil before SyncClient lands, so this
// goes through the getter. An own row before that still records -- with an
// empty sender rather than being dropped, since the frequency and duration
// are the parts worth keeping.
func (a *App) selfCallsign() string {
	if a.st == nil {
		return ""
	}
	return a.st.Snapshot().Self.GetName()
}

// installTXTargetsLocked installs the resolved TX frequency set on the live
// session AND records own-transmission history from the difference.
//
// Every place that installs a target set routes through here -- txPress,
// txRelease and the idle clear -- so a frequency entering the set starts a
// transmission and one leaving it ends one. That yields one row per
// frequency per transmission, which is what actually went on the wire when
// one PTT resolves to two radios.
//
// Hooking the three call sites individually would leave whichever fourth a
// later phase adds silently unlogged, which is exactly the class of gap
// this phase exists to close.
//
// Caller holds voiceState.mu, the same lock the three original call sites
// already held around their SetTXFrequencies store.
func (a *App) installTXTargetsLocked(sess voiceSessionAPI, targets []voice.TXTarget) {
	now := time.Now().UTC()
	next := make(map[voice.KHz]struct{}, len(targets))
	for _, t := range targets {
		next[t.Freq] = struct{}{}
	}
	if a.voice.txStarted == nil {
		a.voice.txStarted = map[voice.KHz]time.Time{}
	}
	for freq := range next {
		if _, live := a.voice.txStarted[freq]; !live {
			a.voice.txStarted[freq] = now
		}
	}
	var ended []struct {
		freq  voice.KHz
		start time.Time
	}
	for freq, start := range a.voice.txStarted {
		if _, still := next[freq]; !still {
			ended = append(ended, struct {
				freq  voice.KHz
				start time.Time
			}{freq, start})
			delete(a.voice.txStarted, freq)
		}
	}

	if sess != nil {
		sess.SetTXFrequencies(targets)
	}

	// Recorded AFTER the session store so a slow history append can never
	// delay the frequency list reaching the wire. The caller's lock IS held
	// across this, though: the critical section now also spans the settings
	// and state-store lock acquisitions, the history append and the emit.
	// Safe only because none of those paths takes voiceState.mu.
	for _, e := range ended {
		a.appendHistory(history.Entry{
			At:         e.start,
			Sender:     a.selfCallsign(),
			FreqKHz:    uint32(e.freq),
			Radio:      a.channelNameFor(e.freq),
			DurationMS: int(now.Sub(e.start) / time.Millisecond),
			Own:        true,
		})
	}
}

func historyEntryDTO(e history.Entry) HistoryEntryDTO {
	return HistoryEntryDTO{
		At:         e.At.Format(time.RFC3339),
		Sender:     e.Sender,
		GUID:       e.GUID,
		FreqKHz:    e.FreqKHz,
		FreqMHz:    float64(e.FreqKHz) / 1000.0,
		Radio:      e.Radio,
		DurationMS: e.DurationMS,
		Own:        e.Own,
	}
}

// GetHistory returns the whole log, newest first, for a window hydrating on
// mount. Always non-nil.
func (a *App) GetHistory() []HistoryEntryDTO {
	out := []HistoryEntryDTO{}
	if a.hist == nil {
		return out
	}
	for _, e := range a.hist.Snapshot() {
		out = append(out, historyEntryDTO(e))
	}
	return out
}

// ClearHistory empties the log.
func (a *App) ClearHistory() {
	if a.hist == nil {
		return
	}
	a.hist.Clear()
	if sb := a.settings; sb != nil && sb.em != nil {
		sb.em.HistoryCleared()
	}
}

// ExportHistoryCSV writes the log to a user-chosen file. A cancelled dialog
// is a normal outcome, not an error.
func (a *App) ExportHistoryCSV() error {
	if a.hist == nil || a.wailsApp == nil {
		return nil
	}
	dst, err := a.wailsApp.Dialog.SaveFile().
		SetMessage("Export transmission log").
		SetFilename("transmission-log.csv").
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dst) == "" {
		return nil
	}
	if err := os.WriteFile(dst, history.CSV(a.hist.Snapshot()), 0o644); err != nil {
		if a.notif != nil {
			a.notif.Raise("history:export", notify.Item{
				Category: "Transmission Log",
				Severity: notify.SeverityError,
				Icon:     "history",
				Title:    "Could not export the transmission log",
				Body:     err.Error(),
			})
		}
		return err
	}
	return nil
}

// NotifyHistoryFlush is history.Log's onErr hook, as main.go wires it.
//
// Package-level for the reason SetNotifier documents: nothing
// package-level is bound by application.NewService, so main.go keeps its
// call and the renderer gains nothing.
func NotifyHistoryFlush(a *App, err error) { a.notifyHistoryFlush(err) }

// notifyHistoryFlush RAISES on a stable key so notify's fingerprint
// collapses a persistent failure into one row rather than one per 5s tick,
// and RESOLVES on the next success.
func (a *App) notifyHistoryFlush(err error) {
	if a.notif == nil {
		return
	}
	if err == nil {
		a.notif.Resolve("history:flush")
		return
	}
	a.notif.Raise("history:flush", notify.Item{
		Category: "Transmission Log",
		Severity: notify.SeverityError,
		Icon:     "history",
		Title:    "Cannot save the transmission log",
		Body:     err.Error(),
	})
}
