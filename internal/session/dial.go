package session

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
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

// insecureDialer returns a Dialer for serverURL. Insecure transport is only
// permitted for localhost/127.0.0.1; remote hosts fail closed until TLS lands.
func insecureDialer(serverURL string) (Dialer, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server address is empty")
	}
	host, _, err := net.SplitHostPort(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server address must be host:port: %w", err)
	}
	if !isLocal(host) {
		return nil, fmt.Errorf("refusing insecure connection to non-local host %q (TLS not yet supported)", host)
	}
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			keepaliveOption(),
			grpc.WithBlock(),
		)
	}, nil
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.")
}
