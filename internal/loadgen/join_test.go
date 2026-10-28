package loadgen

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func newTestJoiner(t *testing.T, f *grpctest.Fake) *Joiner {
	t.Helper()
	dial, cleanup := grpctest.Start(t, f)
	t.Cleanup(cleanup)
	var phase atomic.Int32
	return &Joiner{
		Dial:              dial,
		Hashes:            map[string]string{"Blue": "hash"},
		Unit:              "LDBT",
		AckTimeout:        2 * time.Second,
		Phase:             &phase,
		Resyncs:           &PhaseCounter{},
		ResourceExhausted: &PhaseCounter{},
	}
}

func TestJoinComposedSequence(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "sekret"
	j := newTestJoiner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	want := []string{"InitAuth", "GuestLogin", "SubscribeToUpdates", "SyncClient"}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if bot.Secret != "sekret" {
		t.Fatalf("Secret = %q", bot.Secret)
	}
	if bot.GUID != "guid-test" || bot.Token != "tok-test" {
		t.Fatalf("GUID/Token = %q/%q", bot.GUID, bot.Token)
	}
	if bot.Settings == nil {
		t.Fatal("Settings nil")
	}
	if bot.Store != nil {
		t.Fatal("drain-only bot must not keep a store")
	}
}

func TestJoinFullKeepsStoreAndCounts(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-f", "Blue", true)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	if bot.Store == nil {
		t.Fatal("full bot must keep a store")
	}
	f.PushUpdate(&srspb.ServerUpdate{Type: srspb.ServerUpdate_CLIENT_JOINED})
	deadline := time.Now().Add(2 * time.Second)
	for bot.Counts.Get(srspb.ServerUpdate_CLIENT_JOINED) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := bot.Counts.Get(srspb.ServerUpdate_CLIENT_JOINED); got != 1 {
		t.Fatalf("full bot CLIENT_JOINED count = %d, want 1", got)
	}
}

func TestJoinResyncOnResourceExhausted(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	j.Phase.Store(int32(PhaseHold))
	f.EndStream(status.Error(codes.ResourceExhausted, "update stream fell behind"))
	deadline := time.Now().Add(2 * time.Second)
	for j.Resyncs[PhaseHold].Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := j.Resyncs[PhaseHold].Load(); got != 1 {
		t.Fatalf("Resyncs[hold] = %d, want 1", got)
	}
	if got := j.ResourceExhausted[PhaseHold].Load(); got != 1 {
		t.Fatalf("ResourceExhausted[hold] = %d, want 1", got)
	}
	calls := f.Calls()
	if len(calls) < 2 || !reflect.DeepEqual(calls[len(calls)-2:], []string{"SubscribeToUpdates", "SyncClient"}) {
		t.Fatalf("calls tail = %v", calls)
	}
	if bot.Err() != nil {
		t.Fatalf("bot died after resync: %v", bot.Err())
	}
}

func TestJoinOtherStreamEndKillsBot(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	f.EndStream(status.Error(codes.Unavailable, "gone"))
	deadline := time.Now().Add(2 * time.Second)
	for bot.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if bot.Err() == nil {
		t.Fatal("bot should be dead")
	}
	if j.Resyncs[PhaseRamp].Load() != 0 {
		t.Fatal("no resync expected")
	}
}

func TestJoinAckTimeout(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSubscribeAck(false)
	j := newTestJoiner(t, f)
	j.AckTimeout = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := j.Join(ctx, "bot-1", "Blue", false)
	if err == nil {
		t.Fatal("expected ack timeout error")
	}
	if d := time.Since(start); d > j.AckTimeout+500*time.Millisecond {
		t.Fatalf("took %v", d)
	}
}

func TestJoinUpdateRadio(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.Close(context.Background())
	if err := bot.UpdateRadio(ctx, 251.0); err != nil {
		t.Fatalf("UpdateRadio: %v", err)
	}
	ri := f.LastRadioInfo()
	if ri == nil || len(ri.GetRadios()) != 1 || ri.GetRadios()[0].GetFrequency() != 251.0 || !ri.GetRadios()[0].GetEnabled() {
		t.Fatalf("radio info = %v", ri)
	}
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for !cond() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func TestJoinResyncAckWithheldKillsBot(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	j.AckTimeout = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	f.SetSubscribeAck(false)
	f.EndStream(status.Error(codes.ResourceExhausted, "update stream fell behind"))
	if !waitFor(func() bool { return bot.Err() != nil }) {
		t.Fatal("bot should be dead after a resync whose ack is withheld")
	}
	if j.Resyncs[PhaseRamp].Load() != 0 {
		t.Fatal("a failed resync must not be counted")
	}
	// The failed resync's stream must not linger as a server-side subscriber.
	if !waitFor(func() bool { return f.ActiveStreams() == 0 }) {
		t.Fatalf("ActiveStreams = %d, want 0 after failed resync", f.ActiveStreams())
	}
}

func TestJoinNilCountersDoNotPanic(t *testing.T) {
	f := grpctest.NewFake()
	j := newTestJoiner(t, f)
	j.Resyncs, j.ResourceExhausted = nil, nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := j.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	defer bot.Close(context.Background())
	f.EndStream(status.Error(codes.ResourceExhausted, "update stream fell behind"))
	// The resync completes (SyncClient is the last call) and the bot survives.
	if !waitFor(func() bool {
		c := f.Calls()
		return len(c) >= 2 && c[len(c)-1] == "SyncClient" && c[len(c)-2] == "SubscribeToUpdates"
	}) {
		t.Fatalf("no resync happened: %v", f.Calls())
	}
	time.Sleep(100 * time.Millisecond)
	if bot.Err() != nil {
		t.Fatalf("bot died: %v", bot.Err())
	}
}
