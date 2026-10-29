package loadgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDeathRecordFromError(t *testing.T) {
	cases := []struct {
		name            string
		cause           string
		err             error
		wantCode, wantM string
	}{
		{"status", "stream", status.Error(codes.Unavailable, "error reading from server: connection reset by peer"), "Unavailable", "error reading from server: connection reset by peer"},
		{"nil", "stream", nil, "OK", "update stream ended by the server"},
		{"plain", "stream", errors.New("x"), "Unknown", "x"},
		{"wrapped", "resync", fmt.Errorf("resync: %w", status.Error(codes.Unauthenticated, "s")), "Unauthenticated", "s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewDeathRecord(c.cause, c.err)
			if d.Cause != c.cause || d.Code != c.wantCode || d.Message != c.wantM {
				t.Fatalf("got cause=%q code=%q message=%q", d.Cause, d.Code, d.Message)
			}
			if c.err != nil && d.Error != c.err.Error() {
				t.Fatalf("error = %q, want %q", d.Error, c.err.Error())
			}
			if c.err == nil && d.Error == "" {
				t.Fatal("nil error must still produce a non-empty error text")
			}
		})
	}
}

func TestDeathRecordFixtureRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/interface/v3-control-lost.json")
	if err != nil {
		t.Fatal(err)
	}
	var line DeathLine
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&line); err != nil {
		t.Fatal(err)
	}
	if line.Type != "control_lost" || line.Bot != 2 || line.DeadTotal != 1 || line.SinceLastOKPingS != 3.2 {
		t.Fatalf("decoded %+v", line)
	}
	out, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != string(out) {
		t.Fatalf("round trip differs:\n%s\n%s", raw, out)
	}
}

func TestDeathRecordZeroValuesStillEncoded(t *testing.T) {
	out, _ := json.Marshal(DeathLine{Type: "control_lost"})
	for _, k := range []string{`"ping_failures":0`, `"age_s":0`, `"bot":0`, `"local_addr":""`} {
		if !strings.Contains(string(out), k) {
			t.Fatalf("missing %s in %s", k, out)
		}
	}
}
