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

func TestResolveTrimsWhitespace(t *testing.T) {
	tests := []struct {
		name string
		src  Sources
		want string
	}{
		{"leading space in ServerURL", Sources{ServerURL: " vcs.example.com:14447"}, "vcs.example.com:5002"},
		{"trailing space in ServerURL", Sources{ServerURL: "vcs.example.com:14447 "}, "vcs.example.com:5002"},
		{"leading and trailing space in Update", Sources{Update: " 10.0.0.1:6000 "}, "10.0.0.1:6000"},
		{"whitespace around ConfigHost", Sources{ConfigHost: " 10.0.0.3 ", ConfigPort: 6002}, "10.0.0.3:6002"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.src)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveStripsPathFromSchemeLessServerURL(t *testing.T) {
	got, err := Resolve(Sources{ServerURL: "vcs.example.com/"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "vcs.example.com:5002" {
		t.Fatalf("Resolve = %q, want vcs.example.com:5002", got)
	}
}

func TestResolveRejectsConfigHostWithPort(t *testing.T) {
	_, err := Resolve(Sources{ConfigHost: "10.0.0.3:9999", ConfigPort: 0})
	if err == nil {
		t.Fatal("expected an error when ConfigHost already contains a port")
	}
}

// TestResolveIPv6Literals locks in the bracket convention documented on
// Resolve: bracketed literals resolve correctly (with and without a
// scheme, and via ConfigHost with and without an explicit ConfigPort); a
// bare unbracketed literal is rejected rather than guessed at, because it
// is genuinely ambiguous with "host:port".
func TestResolveIPv6Literals(t *testing.T) {
	tests := []struct {
		name    string
		src     Sources
		want    string
		wantErr bool
	}{
		{"bracketed, no scheme", Sources{ServerURL: "[::1]:14447"}, "[::1]:5002", false},
		{"bracketed, with scheme", Sources{ServerURL: "http://[::1]:14447"}, "[::1]:5002", false},
		{"config host, explicit port", Sources{ConfigHost: "::1", ConfigPort: 6002}, "[::1]:6002", false},
		{"config host, default port", Sources{ConfigHost: "::1", ConfigPort: 0}, "[::1]:5002", false},
		{"bare unbracketed literal via ServerURL is ambiguous", Sources{ServerURL: "::1:14447"}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Resolve(%+v) = %q, want an error", tc.src, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%+v): %v", tc.src, err)
			}
			if got != tc.want {
				t.Fatalf("Resolve(%+v) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestResolveValidatesUpdateAndSync covers finding 4: srs.proto documents
// both VoiceAddressUpdate and ServerSyncResult's voice address fields as
// "host:port", so upstream is responsible for that shape -- but if it is
// ever violated, Resolve must surface a clear error here rather than
// letting a malformed address through to a confusing dial-site failure.
func TestResolveValidatesUpdateAndSync(t *testing.T) {
	if _, err := Resolve(Sources{Update: "no-port-here"}); err == nil {
		t.Fatal("expected an error for an Update value with no port")
	}
	if _, err := Resolve(Sources{Sync: "no-port-here"}); err == nil {
		t.Fatal("expected an error for a Sync value with no port")
	}
}
