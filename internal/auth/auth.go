// Package auth wraps the AuthService gRPC client (guest path for Phase 1).
package auth

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Client wraps srspb.AuthServiceClient.
type Client struct {
	rpc srspb.AuthServiceClient
}

// New builds a Client over an existing gRPC connection.
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{rpc: srspb.NewAuthServiceClient(conn)}
}

// InitResult is the relevant slice of AuthInitResult.
type InitResult struct {
	ClientGUID string
	HasGuest   bool
}

// InitAuth performs the capability handshake.
func (c *Client) InitAuth(ctx context.Context, version string) (InitResult, error) {
	resp, err := c.rpc.InitAuth(ctx, &srspb.AuthInitRequest{
		Capabilities: &srspb.ClientCapabilities{
			Version:                    version,
			SupportedDistributionModes: []srspb.DistributionMode{srspb.DistributionMode_STANDALONE},
		},
	})
	if err != nil {
		return InitResult{}, fmt.Errorf("init auth: %w", err)
	}
	if !resp.GetSuccess() {
		return InitResult{}, fmt.Errorf("init auth: %s", resp.GetErrorMessage())
	}
	r := resp.GetResult()
	return InitResult{ClientGUID: r.GetClientGuid(), HasGuest: r.GetHasGuestLogin()}, nil
}
