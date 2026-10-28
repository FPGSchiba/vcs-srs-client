package loadgen

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The plan fixes these labels verbatim (Global Constraints, "Labels").
func TestLabelsVerbatim(t *testing.T) {
	if ReportLabel != "Shared GitHub runner (4 vCPU), loopback, relative only. Not a capacity figure." {
		t.Errorf("ReportLabel = %q", ReportLabel)
	}
	if PayloadLabel != "synthetic-not-opus" {
		t.Errorf("PayloadLabel = %q", PayloadLabel)
	}
	for _, want := range []string{"closed-loop, <= 16 in flight", "server-side cost only; client hash excluded"} {
		if !strings.Contains(JoinLabel, want) {
			t.Errorf("JoinLabel %q lacks verbatim %q", JoinLabel, want)
		}
	}
}

// A non-ascending step list would leave bots from a larger earlier step
// registered at the reflector, so a later "N" would carry more load than its
// label. It is refused before any socket is opened.
func TestCalibrateRejectsNonAscendingSteps(t *testing.T) {
	allow, err := ParseAllow("127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	for _, ns := range [][]int{{8, 4}, {4, 4}, {1, 4}, {}} {
		start := time.Now()
		_, _, err := Calibrate(context.Background(), "127.0.0.1:9", allow, 1, ns, time.Second)
		if err == nil {
			t.Fatalf("Calibrate(ns=%v) = nil error", ns)
		}
		if el := time.Since(start); el > 100*time.Millisecond {
			t.Fatalf("Calibrate(ns=%v) took %v; validation must precede any dial", ns, el)
		}
	}
}
