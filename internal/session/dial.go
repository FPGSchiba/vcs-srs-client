package session

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// clientKeepalive* are the gRPC transport keepalive parameters.
//
// The server sets KeepaliveEnforcementPolicy{MinTime: 60s} on the gRPC
// server that serves SRSService (vngd-srs-server control/server.go), so a
// client pinging more often than that earns a GOAWAY with too_many_pings and
// loses the connection -- an availability feature turned into an outage.
// 75s leaves margin against that floor, since a ping arriving marginally
// early still counts against the policy.
//
// This is a BACKSTOP, not the detector. The server's own ServerParameters
// already drop a dead client at roughly 70s; the application-level Ping in
// connhealth answers in about 15s, and that is what the status surface
// depends on. This only covers the idle case more cheaply.
const (
	clientKeepaliveTime    = 75 * time.Second
	clientKeepaliveTimeout = 10 * time.Second
)

// ClientKeepaliveTime exposes the configured keepalive period so a test can
// pin it against the server's enforcement floor. See clientKeepaliveTime.
func ClientKeepaliveTime() time.Duration { return clientKeepaliveTime }

func keepaliveOption() grpc.DialOption {
	return grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    clientKeepaliveTime,
		Timeout: clientKeepaliveTimeout,
		// Matches the server's PermitWithoutStream: true. The update stream
		// is long-lived, so this rarely matters, but it is the correct
		// pairing and costs nothing.
		PermitWithoutStream: true,
	})
}

// transportCredentials picks the transport for host, honouring caFile.
//
// Three deterministic branches, in this order:
//
//  1. caFile set    -> TLS trusting ONLY that pool, whatever the host. This
//     is what a self-hosted server with no public DNS and no CA-signed
//     certificate needs, and because it is not gated on the host it doubles
//     as the local-TLS development and test path.
//  2. host loopback -> insecure, unchanged from Phase 1.
//  3. otherwise     -> TLS against the OS trust store.
//
// There is deliberately no plaintext-remote escape hatch: the Phase 1
// fail-closed rule inverts rather than relaxes, so what used to be refused
// outright is now required to be encrypted. There is equally no fallback to
// plaintext when a handshake fails, because a fallback is a downgrade attack
// with extra steps.
//
// A caFile that cannot be read, or that holds no certificate, is an ERROR
// rather than a quiet fall-through to system roots. An explicit pin that
// silently stops pinning is the precise failure this sub-phase exists to
// prevent.
func transportCredentials(host, caFile string) (credentials.TransportCredentials, error) {
	if caFile != "" {
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read tls_ca_file %q: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("tls_ca_file %q contains no PEM certificate", caFile)
		}
		// MinVersion is set explicitly (and again below) even though
		// credentials.NewTLS already defaults an unset MinVersion to TLS 1.2
		// itself: this future-proofs against a grpc-go that stops doing so.
		// It has no dedicated test -- grpc-go's identical default makes the
		// two indistinguishable from any public accessor, handshake, or even
		// reflection into the credentials' own private config, so no test
		// could tell "we set it" apart from "grpc-go defaulted it" and catch
		// a regression here.
		return credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}), nil
	}
	if isLocal(host) {
		return insecure.NewCredentials(), nil
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
}

// dialerFor returns a Dialer for serverURL with credentials chosen by
// transportCredentials. It replaces Phase 1's insecureDialer.
func dialerFor(serverURL, caFile string) (Dialer, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server address is empty")
	}
	host, _, err := net.SplitHostPort(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server address must be host:port: %w", err)
	}
	creds, err := transportCredentials(host, caFile)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		conn, err := grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(creds),
			keepaliveOption(),
			// Replaces WithBlock. It blocks identically, but returns the last
			// connection error instead of a bare context deadline. Without it
			// every TLS failure reaches the user as "context deadline
			// exceeded" and explainDialError has nothing to work with.
			grpc.WithReturnConnectionError(),
		)
		if err != nil {
			return nil, explainDialError(err, serverURL, caFile)
		}
		return conn, nil
	}, nil
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.")
}

// explainDialError turns a gRPC transport failure into something a user can
// act on.
//
// The cases are matched on crypto/tls and crypto/x509 error TEXT because
// gRPC surfaces them as an opaque wrapped string with no typed cause to
// inspect -- there is no errors.As target available here. That is fragile by
// nature, so the default is the original error: a change in Go's wording
// degrades the message rather than hiding the failure or mislabelling it.
//
// The original is always wrapped, never replaced, so errors.Is still reaches
// it for the log.
func explainDialError(err error, serverURL, caFile string) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "first record does not look like a TLS handshake"):
		return fmt.Errorf("%s is not speaking TLS -- the server most likely has no clientTLS block configured: %w", serverURL, err)

	case strings.Contains(s, "certificate is valid for"):
		return fmt.Errorf("%s presented a certificate for a different name -- connect by the server's hostname, or have its certificate reissued with this address in the SANs: %w", serverURL, err)

	case strings.Contains(s, "certificate signed by unknown authority"),
		strings.Contains(s, "failed to verify certificate"):
		if caFile != "" {
			return fmt.Errorf("%s presented a certificate not issued by the authority in tls_ca_file %q: %w", serverURL, caFile, err)
		}
		return fmt.Errorf("%s presented a certificate that no system root trusts -- if this is a self-hosted server, point tls_ca_file at its certificate: %w", serverURL, err)
	}
	return err
}
