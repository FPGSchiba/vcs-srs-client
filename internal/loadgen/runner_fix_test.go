package loadgen

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestHealthReportCarriesSendErrors(t *testing.T) {
	r := NewHealthReport(Health{SendErrors: 3, GenCPU: -1})
	if r.SendErrors != 3 {
		t.Fatalf("SendErrors = %d, want 3", r.SendErrors)
	}
}

func TestPhaseDeltaHoldOnly(t *testing.T) {
	base := PhaseCounts{Ramp: 5, Settle: 2, Hold: 4}
	now := PhaseCounts{Ramp: 5, Settle: 2, Hold: 9}
	got := holdDelta(now, base)
	if got.Hold != 5 || got.Ramp != 5 || got.Settle != 2 {
		t.Fatalf("holdDelta = %+v", got)
	}
}

func TestServeHonoursContextCancelWithBlockedStdin(t *testing.T) {
	r, err := NewRunner(RunConfig{Scenario: Scenario{Name: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx, pr, io.Discard) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Serve err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel with stdin blocked")
	}
}
