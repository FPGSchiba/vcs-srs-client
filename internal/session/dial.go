package session

import (
	"context"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
			grpc.WithBlock(),
		)
	}, nil
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.")
}
