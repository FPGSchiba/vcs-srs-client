package voice

import "testing"

func TestResolvePrecedence(t *testing.T) {
	full := Sources{
		Update:     "10.0.0.1:6000",
		Sync:       "10.0.0.2:6001",
		ConfigHost: "10.0.0.3",
		ConfigPort: 6002,
		ServerURL:  "vcs.example.com:14447",
	}

	tests := []struct {
		name string
		mut  func(*Sources)
		want string
	}{
		{"update wins", func(s *Sources) {}, "10.0.0.1:6000"},
		{"sync when no update", func(s *Sources) { s.Update = "" }, "10.0.0.2:6001"},
		{"config when no server address", func(s *Sources) { s.Update, s.Sync = "", "" }, "10.0.0.3:6002"},
		{"derived from server_url when nothing else", func(s *Sources) {
			s.Update, s.Sync, s.ConfigHost = "", "", ""
		}, "vcs.example.com:5002"},
		{"config host with no port uses the default", func(s *Sources) {
			s.Update, s.Sync, s.ConfigPort = "", "", 0
		}, "10.0.0.3:5002"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := full
			tc.mut(&s)
			got, err := Resolve(s)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveTreatsEmptyServerAddressesAsAbsent is the case that actually
// happens today. getVoiceAddresses delegates to a registry populated ONLY by
// distributed voice nodes calling RegisterVoiceServer; a standalone server
// never registers itself, so both fields come back as empty strings on every
// standalone deployment. Treating "" as a usable address would make the
// client dial ":0".
func TestResolveTreatsEmptyServerAddressesAsAbsent(t *testing.T) {
	got, err := Resolve(Sources{Update: "", Sync: "", ServerURL: "localhost:14447"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "localhost:5002" {
		t.Fatalf("Resolve = %q, want localhost:5002", got)
	}
}

func TestResolveStripsSchemeFromServerURL(t *testing.T) {
	for _, in := range []string{"http://vcs.example.com:14447", "vcs.example.com:14447", "vcs.example.com"} {
		got, err := Resolve(Sources{ServerURL: in})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", in, err)
		}
		if got != "vcs.example.com:5002" {
			t.Fatalf("Resolve(%q) = %q, want vcs.example.com:5002", in, got)
		}
	}
}

func TestResolveErrorsWithNothingToGoOn(t *testing.T) {
	if _, err := Resolve(Sources{}); err == nil {
		t.Fatal("expected an error when no source yields a host")
	}
}
