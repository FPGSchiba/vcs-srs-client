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

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a certificate signed by an untrusted issuer to be refused")
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

	conn, err := dialWith(dialCtx(t), clientCreds)
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

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a TLS client against a plaintext server to fail")
	}
}
