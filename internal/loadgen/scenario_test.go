package loadgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestLoadScenarioOneBigNet(t *testing.T) {
	s, err := LoadScenario("one-big-net")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "one-big-net" || s.Version != 1 || s.Seed != 1 || s.Coalition != "Blue" || s.FreqMHz != 251.0 {
		t.Fatalf("got %+v", s)
	}
	if s.FreqKHz() != 251000 {
		t.Fatalf("khz = %d", s.FreqKHz())
	}
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte(`{"name":"x","version":1,"seed":1,"coalition":"Blue","freq_mhz":251.0,"bogus":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadScenario(p); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestCheckAgainstServer(t *testing.T) {
	srv := &srspb.ServerSettings{GlobalFrequencies: []float32{200}, TestFrequencies: []float32{100}}
	s := Scenario{FreqMHz: 251.0}
	if err := s.CheckAgainstServer(srv); err != nil {
		t.Fatal(err)
	}
	if err := (Scenario{FreqMHz: 200.0}).CheckAgainstServer(srv); err == nil || !strings.Contains(err.Error(), "global") {
		t.Fatalf("err = %v", err)
	}
	if err := (Scenario{FreqMHz: 100.0}).CheckAgainstServer(srv); err == nil || !strings.Contains(err.Error(), "test") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadScenarioValidation(t *testing.T) {
	cases := map[string]string{
		"zero freq":     `{"name":"x","version":1,"seed":1,"coalition":"Blue","freq_mhz":0}`,
		"negative freq": `{"name":"x","version":1,"seed":1,"coalition":"Blue","freq_mhz":-5}`,
		"no coalition":  `{"name":"x","version":1,"seed":1,"coalition":"","freq_mhz":251}`,
		"trailing json": `{"name":"x","version":1,"seed":1,"coalition":"Blue","freq_mhz":251} {"a":1}`,
	}
	for name, body := range cases {
		p := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadScenario(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
