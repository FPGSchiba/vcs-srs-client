package session

import (
	"context"
	"crypto/tls"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

func dialCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// refusalCtx is for the tests below that expect the handshake to be refused.
// grpc.DialContext without WithBlock retries with backoff until the context
// deadline, and grpc.WithReturnConnectionError only surfaces the underlying
// error once that deadline is hit -- so the wait is bounded by this
// deadline, not by how fast the handshake itself fails. 500ms was confirmed
// to still surface the identical handshake errors these tests assert on;
// using it instead of dialCtx's 5s keeps three refusal tests from adding
// ~15s to every run of this package.
func refusalCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestTLS_TrustedCertificateConnects(t *testing.T) {
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&cert.Certificate))
	t.Cleanup(cleanup)

	// The client credentials come from transportCredentials itself, not a
	// hand-built tls.Config, because that is the only way this test proves
	// the parsed CA pool is actually installed as the trust root. A
	// transportCredentials that read and parsed the CA file and then threw
	// the pool away would still pass a test built on credentials.NewTLS
	// directly.
	caPath := cert.WriteCAPEM(t, t.TempDir())
	creds, err := transportCredentials("localhost", caPath)
	if err != nil {
		t.Fatalf("transportCredentials: %v", err)
	}
	// bufconn's dial target is the literal string "bufnet", which is not a
	// SAN on the certificate, so the ServerName transportCredentials would
	// otherwise derive from the dial target must be overridden -- the same
	// override a client pinning by hostname relies on.
	if err := creds.OverrideServerName("localhost"); err != nil {
		t.Fatalf("OverrideServerName: %v", err)
	}

	conn, err := dialWith(dialCtx(t), creds)
	if err != nil {
		t.Fatalf("expected the handshake to succeed against a trusted cert: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}

func TestTLS_UntrustedCertificateIsRefused(t *testing.T) {
	serverCert := grpctest.NewTestCert(t, "localhost")
	otherCert := grpctest.NewTestCert(t, "localhost") // a different issuer

	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&serverCert.Certificate))
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    otherCert.Pool(), // pins the WRONG issuer
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(refusalCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a certificate signed by an untrusted issuer to be refused")
	}
	// Pins the failure to the untrusted-issuer verification path, not merely
	// to "some error" -- a broken harness (e.g. a server that never started)
	// would also produce a non-nil error here.
	if !strings.Contains(err.Error(), "unknown authority") {
		t.Fatalf("expected an unknown-authority verification failure, got: %v", err)
	}
}

func TestTLS_HostnameMismatchIsRefused(t *testing.T) {
	cert := grpctest.NewTestCert(t, "srs.example.org") // no "localhost" SAN
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&cert.Certificate))
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(), // issuer IS trusted; only the name is wrong
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(refusalCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a certificate valid for a different name to be refused")
	}
	if !strings.Contains(err.Error(), "x509") {
		t.Fatalf("expected an x509 verification failure, got: %v", err)
	}
}

func TestTLS_ClientAgainstPlaintextServerFails(t *testing.T) {
	// The failure an operator hits when they upgrade the client before the
	// server, or forget the clientTLS block entirely.
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, nil) // plaintext server
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(refusalCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a TLS client against a plaintext server to fail")
	}
	// Pins the failure to the TLS client speaking to a plaintext peer, not
	// merely to "some error" -- a broken harness (e.g. a closed bufconn)
	// would also produce a non-nil error here.
	if !strings.Contains(err.Error(), "first record does not look like a TLS handshake") {
		t.Fatalf("expected a not-a-TLS-handshake failure, got: %v", err)
	}
}

func TestTLS_PlaintextServerProducesTheExplainedError(t *testing.T) {
	// Proves the wrapping is actually reached through the real dial path,
	// not merely unit-tested in isolation -- and that
	// WithReturnConnectionError really does surface the TLS cause rather
	// than a context deadline.
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, nil) // plaintext
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	_, err := dialWith(refusalCtx(t), clientCreds)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}
	explained := explainDialError(err, "srs.example.org:5002", "")
	if !strings.Contains(explained.Error(), "not speaking TLS") {
		t.Fatalf("expected the plaintext-server diagnosis, got: %v", explained)
	}
}
