package loadgen

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

func TestSplitDests(t *testing.T) {
	got := SplitDests(10, 3)
	want := [][2]int{{0, 4}, {4, 7}, {7, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitDests(10,3) = %v, want %v", got, want)
	}
	covered := 0
	r := SplitDests(1, 4)
	if len(r) != 4 {
		t.Fatalf("len = %d, want 4", len(r))
	}
	prev := 0
	for _, x := range r {
		if x[0] != prev || x[1] < x[0] {
			t.Fatalf("not contiguous: %v", r)
		}
		covered += x[1] - x[0]
		prev = x[1]
	}
	if covered != 1 || prev != 1 {
		t.Fatalf("covers %d ending %d, want exactly 1: %v", covered, prev, r)
	}
}

func TestDeterministicUUID(t *testing.T) {
	a, b := DeterministicUUID(7, 3), DeterministicUUID(7, 3)
	if a != b {
		t.Fatal("not stable")
	}
	if DeterministicUUID(7, 4) == a || DeterministicUUID(8, 3) == a {
		t.Fatal("collision across i or seed")
	}
	if a.Version() != 4 {
		t.Fatalf("version = %d", a.Version())
	}
}

func startReflector(t *testing.T, senders int) *Reflector {
	t.Helper()
	r, err := NewReflector("127.0.0.1:0", senders)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r
}

func TestNewReflectorRejectsOneSender(t *testing.T) {
	if _, err := NewReflector("127.0.0.1:0", 1); err == nil {
		t.Fatal("senders=1 accepted")
	}
}

func TestReflectorFansOutOnce(t *testing.T) {
	r := startReflector(t, 2)
	const freq = voice.KHz(251000)
	var bots []*VoiceConn
	for i := 0; i < 4; i++ {
		v, err := DialVoice(r.Addr(), DeterministicUUID(1, i))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(v.Close)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if _, err := v.Hello(ctx, "", DefaultHelloLadder); err != nil {
			cancel()
			t.Fatalf("hello %d: %v", i, err)
		}
		cancel()
		v.Start(context.Background())
		bots = append(bots, v)
	}
	for i, v := range bots {
		v.ExpectFrom(bots[0].ID, freq, 1, 20)
		_ = i
	}
	for seq := uint32(1); seq <= 20; seq++ {
		if err := bots[0].Send(seq, freq, SyntheticFrame(seq)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond)
	for i := 1; i < 4; i++ {
		lr, ok := bots[i].LedgerResult(bots[0].ID, freq)
		if !ok || lr.Unique != 20 || lr.Dups != 0 {
			t.Errorf("bot %d: %+v ok=%v, want 20 unique 0 dups", i, lr, ok)
		}
	}
	if lr, _ := bots[0].LedgerResult(bots[0].ID, freq); lr.Unique != 0 {
		t.Errorf("sender got %d of its own frames", lr.Unique)
	}
	if r.SendErrors() != 0 {
		t.Errorf("send errors = %d", r.SendErrors())
	}
}

func TestCalibrateSmall(t *testing.T) {
	r := startReflector(t, 2)
	allow, err := ParseAllow(r.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	steps, capy, err := Calibrate(ctx, r.Addr(), allow, 1, []int{4, 8}, time.Second,
		WithReflectorErrors(r.SendErrors))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	for _, s := range steps {
		want := 50 * float64(s.N-1)
		if math.Abs(s.EgressPPS-want) > 0.2*want {
			t.Errorf("N=%d pps=%.1f, want %.0f +-20%%", s.N, s.EgressPPS, want)
		}
		if s.Violations == nil {
			t.Errorf("N=%d nil violations", s.N)
		}
	}
	_ = capy // validity is not asserted (shared Windows CI)
	if bad, err := ParseAllow("127.0.0.1:1"); err == nil {
		if _, _, err := Calibrate(ctx, r.Addr(), bad, 1, []int{4}, time.Second); err == nil {
			t.Fatal("calibrate dialled a non-allowed reflector")
		}
	}
	_ = fmt.Sprint
}
