package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/FPGSchiba/vcs-srs-client/srspb"
	"github.com/google/uuid"
)

func TestOnVoiceRXRecordsAResolvedRow(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))

	id := uuid.New()
	a.st.UpdateClient(id.String(), &srspb.ClientInfo{Name: "Dabble"})
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()

	start := time.Date(2026, 9, 30, 21, 15, 48, 0, time.UTC)
	a.onVoiceRX(voice.RXEvent{Sender: id, Freq: 118500, Start: start, End: start.Add(3200 * time.Millisecond)})

	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatalf("history = %+v, want 1 row", got)
	}
	if got[0].Sender != "Dabble" || got[0].Radio != "Fleet Common" || got[0].DurationMS != 3200 || got[0].Own {
		t.Fatalf("row = %+v", got[0])
	}
}

func TestResolveSenderHandlesNonCanonicalStoreKey(t *testing.T) {
	// The store is keyed by the SERVER's GUID string while voice carries a
	// uuid.UUID. uuid.Parse accepts uppercase, braced and unhyphenated
	// forms that UUID.String() does not round-trip to, so a naive
	// sender.String() lookup can silently render a blank sender.
	a := newTestAppWithConfig(t, config.Default(), "")
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	a.st.UpdateClient(strings.ToUpper(id.String()), &srspb.ClientInfo{Name: "Shouty"})

	guid, callsign := a.resolveSender(id)
	if guid != strings.ToUpper(id.String()) {
		t.Fatalf("guid = %q, want the store's own key %q", guid, strings.ToUpper(id.String()))
	}
	if callsign != "Shouty" {
		t.Fatalf("callsign = %q, want Shouty -- the index must normalise GUID spelling", callsign)
	}
	if guid == "" {
		t.Fatal("guid must be the store's own key so the row can be traced back")
	}
}

func TestOnVoiceRXUnknownSenderStillRecords(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	start := time.Now().UTC()
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500, Start: start, End: start.Add(time.Second)})
	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatal("a transmission from a client that has already left must still record: the frequency and duration are worth keeping")
	}
	if got[0].Sender != "" {
		t.Fatalf("Sender = %q, want empty rather than a fabricated name", got[0].Sender)
	}
}

func TestGlobalChannelRowHasNoChannelName(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	// No local radio on 200000 kHz: a server global channel is accepted
	// without one.
	start := time.Now().UTC()
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 200000, Start: start, End: start.Add(time.Second)})
	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatalf("history = %+v, want 1 row", got)
	}
	if got[0].Radio != "" {
		t.Fatalf("Radio = %q, want empty -- inventing a channel name for a global would be a lie", got[0].Radio)
	}
}

func TestInstallTXTargetsRecordsOneRowPerFrequency(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	a.st.SetSelf("me", &srspb.ClientInfo{Name: "FPGSchiba"})
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{
		{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true},
	}
	sb.cfg = &next
	sb.mu.Unlock()

	fake := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}, {Freq: 122750}})
	a.voice.mu.Unlock()

	if got := a.GetHistory(); len(got) != 0 {
		t.Fatalf("nothing is logged while transmitting: %+v", got)
	}

	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, nil)
	a.voice.mu.Unlock()

	got := a.GetHistory()
	if len(got) != 2 {
		t.Fatalf("two targets released together must produce two rows, got %d: %+v", len(got), got)
	}
	seen := map[uint32]int{}
	for _, r := range got {
		if !r.Own || r.Sender != "FPGSchiba" {
			t.Fatalf("own rows must carry the local callsign: %+v", r)
		}
		seen[r.FreqKHz]++
	}
	if seen[118500] != 1 || seen[122750] != 1 {
		t.Fatalf("want one row for each of 118500 and 122750, got %v", seen)
	}
}

func TestInstallTXTargetsPartialReleaseLogsOnlyTheReleasedOne(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{
		{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true},
	}
	sb.cfg = &next
	sb.mu.Unlock()

	fake := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}, {Freq: 122750}})
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}})
	a.voice.mu.Unlock()

	got := a.GetHistory()
	if len(got) != 1 || got[0].FreqKHz != 122750 {
		t.Fatalf("only the released frequency logs: %+v", got)
	}
}

func TestOwnRowWithNoSelfStillRecords(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	fake := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}})
	a.installTXTargetsLocked(fake, nil)
	a.voice.mu.Unlock()
	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatal("a row before SyncClient lands must still record")
	}
	if got[0].Sender != "" {
		t.Fatalf("Sender = %q, want empty", got[0].Sender)
	}
}

func TestClearHistoryEmpties(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	rec := &recordingEmitter{}
	a.settings.em = events.New(rec)
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500,
		Start: time.Now(), End: time.Now().Add(time.Second)})
	if len(a.GetHistory()) != 1 {
		t.Fatal("precondition: a row must exist before clearing")
	}
	a.ClearHistory()
	if rec.count(events.EventHistoryCleared) != 1 {
		t.Fatalf("HistoryCleared emitted %d times, want 1", rec.count(events.EventHistoryCleared))
	}
	if got := a.GetHistory(); len(got) != 0 {
		t.Fatalf("history = %+v, want empty", got)
	}
}

func TestGetHistoryWithNoLogIsEmptyNotNil(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	if got := a.GetHistory(); got == nil {
		t.Fatal("must be a non-nil empty slice: the frontend types it as an array and nil marshals to null")
	}
}

func twoRadios(a *App) {
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{
		{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true},
	}
	sb.cfg = &next
	sb.mu.Unlock()
}

func setSess(a *App, s voiceSessionAPI) {
	a.voice.mu.Lock()
	a.voice.sess = s
	a.voice.mu.Unlock()
}

// The central claim: the REAL press, release and idle-clear paths all log.
func TestPressReleaseAndIdleClearPathsLogRows(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	twoRadios(a)
	setSess(a, &fakeVoiceSession{})

	// txRelease with another action still held installs the shrunk set.
	a.txPress("radio.1.ptt")
	a.txPress("radio.2.ptt")
	if held := a.txRelease("radio.2.ptt"); !held {
		t.Fatal("radio.1.ptt should still be held")
	}
	got := a.GetHistory()
	if len(got) != 1 || got[0].FreqKHz != 122750 {
		t.Fatalf("txRelease path must log the released frequency: %+v", got)
	}

	// Final release + idle clear logs the last one.
	a.txRelease("radio.1.ptt")
	a.clearTXTargetsIfStillIdle()
	got = a.GetHistory()
	if len(got) != 2 || got[0].FreqKHz != 118500 {
		t.Fatalf("idle-clear path must log the last frequency: %+v", got)
	}
}

func TestHistoryAppendedEmitsExactlyOneEntry(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	rec := &recordingEmitter{}
	a.settings.em = events.New(rec)
	start := time.Now().UTC()
	for i := 0; i < 3; i++ {
		a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500, Start: start, End: start.Add(time.Second)})
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for i, name := range rec.events {
		if name != events.EventHistoryAppended {
			continue
		}
		n++
		if _, ok := rec.payloads[i].(HistoryEntryDTO); !ok {
			t.Fatalf("payload %d is %T, want one HistoryEntryDTO (not a snapshot)", i, rec.payloads[i])
		}
	}
	if n != 3 {
		t.Fatalf("emitted %d times, want 3", n)
	}
}

func TestTeardownFlushesInFlightTransmission(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	twoRadios(a)
	rec := &recordingEmitter{}
	a.settings.em = events.New(rec)
	setSess(a, &fakeVoiceSession{})
	a.txPress("radio.1.ptt")
	if len(a.GetHistory()) != 0 {
		t.Fatal("nothing logs while transmitting")
	}
	a.setVoiceSession(nil, 0)
	got := a.GetHistory()
	if len(got) != 1 || !got[0].Own || got[0].FreqKHz != 118500 {
		t.Fatalf("teardown must flush the in-flight row: %+v", got)
	}
	if rec.count(events.EventHistoryAppended) != 1 {
		t.Fatalf("teardown emitted %d appends, want 1", rec.count(events.EventHistoryAppended))
	}
}

func TestStaleTXStartDoesNotSurviveTeardown(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), "")
	a.setHistory(history.New(10))
	twoRadios(a)
	setSess(a, &fakeVoiceSession{})
	a.txPress("radio.1.ptt")
	a.setVoiceSession(nil, 0)
	time.Sleep(150 * time.Millisecond)

	setSess(a, &fakeVoiceSession{})
	a.txPress("radio.1.ptt")
	a.txRelease("radio.1.ptt")
	a.clearTXTargetsIfStillIdle()
	got := a.GetHistory() // newest first
	if len(got) != 2 {
		t.Fatalf("want teardown row + second row, got %+v", got)
	}
	if got[0].DurationMS >= 100 {
		t.Fatalf("second row duration %dms spans the teardown gap (stale txStarted)", got[0].DurationMS)
	}
}
