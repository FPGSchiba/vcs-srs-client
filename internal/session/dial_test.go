package session

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

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

// TestDialerFor_BoundsAnUnreachableDial proves boundedDial -- the function
// behind dialerFor's returned closure -- returns even when the CALLER passes
// context.Background(), as internal/app's Connect and Reconnect do. Without
// the context.WithTimeout(ctx, dialTimeout) wrapping in boundedDial, a
// grpc.DialContext using grpc.WithReturnConnectionError() retries a
// persistently-failing dial forever, blocking on a background context that
// never expires -- and explainDialError's diagnosis is never reached because
// the dial never returns.
//
// The "unreachable target" here is a bufconn.Listener nobody ever Accepts
// on: DialContext sends the new connection on an UNBUFFERED channel that
// only Accept reads from, so with no Accept call that send -- and therefore
// the whole dial -- blocks until the context is done. A real closed socket
// would demonstrate the same thing, but net.Listen and net.Dial to a real
// address are blocked by bind/connect restrictions in the development
// sandbox this was written in (see grpctest.StartWith's doc comment for the
// identical constraint); an unaccepted bufconn achieves the same "never
// resolves on its own" property with no real socket at all.
//
// dialTimeout is shrunk here so this test does not spend the real 10s
// production value on every run; the mechanism under test -- a caller
// context with no deadline of its own still gets bounded -- is identical at
// any duration. Removing the context.WithTimeout call in boundedDial makes
// this test hang until its own guard fires and fail.
func TestDialerFor_BoundsAnUnreachableDial(t *testing.T) {
	orig := dialTimeout
	dialTimeout = 200 * time.Millisecond
	t.Cleanup(func() { dialTimeout = orig })

	lis := bufconn.Listen(1024)
	t.Cleanup(func() { _ = lis.Close() })
	neverAccepted := grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(c)
	})

	done := make(chan error, 1)
	go func() {
		_, dialErr := boundedDial(context.Background(), "unreachable", insecure.NewCredentials(), neverAccepted)
		done <- dialErr
	}()

	select {
	case dialErr := <-done:
		if dialErr == nil {
			t.Fatal("expected the dial against an unreachable target to fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dial did not return within the guard -- context.Background() from the caller is not being bounded by dialTimeout")
	}
}
