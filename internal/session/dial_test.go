package session

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"google.golang.org/grpc/credentials"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

// isTLS reports whether creds are TLS rather than insecure, by the protocol
// name the credentials advertise. insecure.NewCredentials() reports "insecure";
// credentials.NewTLS reports "tls".
func isTLS(c credentials.TransportCredentials) bool {
	return c.Info().SecurityProtocol == "tls"
}

func TestTransportCredentials_BranchSelection(t *testing.T) {
	// A real certificate on disk, so the pinned branch is exercised with
	// something AppendCertsFromPEM genuinely accepts rather than a fixture
	// that could drift out of validity.
	caPath := grpctest.NewTestCert(t, "localhost").WriteCAPEM(t, t.TempDir())

	tests := []struct {
		name    string
		host    string
		caFile  string
		wantTLS bool
	}{
		{"loopback name is insecure", "localhost", "", false},
		{"loopback v4 is insecure", "127.0.0.1", "", false},
		{"loopback v4 subnet is insecure", "127.0.0.53", "", false},
		{"loopback v6 is insecure", "::1", "", false},
		{"remote name uses TLS", "srs.example.org", "", true},
		{"remote v4 uses TLS", "203.0.113.10", "", true},
		{"remote v6 uses TLS", "2001:db8::1", "", true},
		{"LAN address is remote, so TLS", "192.168.1.10", "", true},
		{"pinned CA forces TLS on loopback", "localhost", caPath, true},
		{"pinned CA on a remote host uses TLS", "srs.example.org", caPath, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			creds, err := transportCredentials(tc.host, tc.caFile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := isTLS(creds); got != tc.wantTLS {
				t.Fatalf("host=%q caFile set=%v: wantTLS=%v got=%v", tc.host, tc.caFile != "", tc.wantTLS, got)
			}
		})
	}
}

func TestTransportCredentials_BadCAFile(t *testing.T) {
	dir := t.TempDir()

	emptyPath := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(emptyPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	junkPath := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junkPath, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name    string
		caFile  string
		wantErr string
	}{
		// Review Focus 1: a missing file must fail loudly. Falling back to
		// system roots here would silently ignore an explicit pin.
		{"missing file", filepath.Join(dir, "nope.pem"), "read tls_ca_file"},
		// Review Focus 2: an empty or unparseable file must be distinct from
		// "missing", and must not yield an empty pool that rejects everything
		// with a confusing verification error at dial time.
		{"empty file", emptyPath, "no PEM certificate"},
		{"not a certificate", junkPath, "no PEM certificate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := transportCredentials("srs.example.org", tc.caFile)
			if err == nil {
				t.Fatal("expected an error, got nil -- a bad pin must never fall back to system roots")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// tlsConfigOf reaches into the unexported *tls.Config field grpc-go's
// credentials.NewTLS stores on its concrete (unexported) type, via
// reflection plus unsafe.Pointer to step around the unexported-field read
// guard.
//
// This exists because credentials.TransportCredentials has no public
// accessor for the tls.Config it was built from: Info() reports a hardcoded
// "SecurityVersion: 1.2" string rather than the actual configured value, and
// Clone()/OverrideServerName() do not expose it either. A handshake-level
// test cannot substitute: grpc-go's own credentials.NewTLS already defaults
// MinVersion to tls.VersionTLS12 whenever it is left unset, so removing the
// explicit MinVersion field from transportCredentials would not change
// negotiated behaviour and no such test would notice the regression. This
// reflection reach is fragile against a grpc-go internal rename (it is
// pinned to the field name "config"), but it is the only way to assert what
// this package's own code actually wrote into the struct.
func tlsConfigOf(t *testing.T, creds credentials.TransportCredentials) *tls.Config {
	t.Helper()
	v := reflect.ValueOf(creds)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName("config")
	if !f.IsValid() {
		t.Fatalf("credentials value of type %T has no field named %q; grpc-go internals may have changed", creds, "config")
	}
	f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem() //nolint:gosec // test-only reflection to assert a private field.
	cfg, ok := f.Interface().(*tls.Config)
	if !ok {
		t.Fatalf("field %q is %T, want *tls.Config", "config", f.Interface())
	}
	return cfg
}

func TestTransportCredentials_MinVersionTLS12(t *testing.T) {
	// Both TLS branches transportCredentials can take -- the pinned-CA
	// branch and the remote-without-a-pin branch -- must carry
	// MinVersion: tls.VersionTLS12. See tlsConfigOf for why this is checked
	// by reflection rather than through the public interface or a handshake.
	caPath := grpctest.NewTestCert(t, "localhost").WriteCAPEM(t, t.TempDir())

	tests := []struct {
		name   string
		host   string
		caFile string
	}{
		{"pinned CA branch", "localhost", caPath},
		{"remote, no pin, branch", "srs.example.org", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			creds, err := transportCredentials(tc.host, tc.caFile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			cfg := tlsConfigOf(t, creds)
			if cfg.MinVersion != tls.VersionTLS12 {
				t.Fatalf("MinVersion = %v, want tls.VersionTLS12", cfg.MinVersion)
			}
		})
	}
}

func TestDialerFor_AddressValidation(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		wantErr   string // substring; "" means a dialer is returned
	}{
		{"empty", "", "empty"},
		{"no port", "localhost", "host:port"},
		{"localhost ok", "localhost:5002", ""},
		{"127.0.0.1 ok", "127.0.0.1:5002", ""},
		{"ipv6 loopback ok", "[::1]:5002", ""},
		// Review Focus 4: previously rejected outright. Now it is accepted
		// and will be dialed over TLS -- the fail-closed rule inverted
		// rather than relaxed.
		{"remote accepted, over TLS", "srs.example.org:5002", ""},
		{"remote ipv6 accepted", "[2001:db8::1]:5002", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := dialerFor(tc.serverURL, "")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected a dialer, got error: %v", err)
				}
				if d == nil {
					t.Fatal("expected non-nil dialer")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
