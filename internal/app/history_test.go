package app

import (
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
	if got := a.GetHistory(); got[0].Radio != "" {
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
	for _, r := range got {
		if !r.Own || r.Sender != "FPGSchiba" {
			t.Fatalf("own rows must carry the local callsign: %+v", r)
		}
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
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500,
		Start: time.Now(), End: time.Now().Add(time.Second)})
	a.ClearHistory()
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
