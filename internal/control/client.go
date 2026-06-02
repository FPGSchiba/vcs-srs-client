// Package control wraps the SRSService gRPC client and its update stream.
package control

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// tokenCreds attaches the guest token as gRPC metadata on every call.
type tokenCreds struct{ token string }

func (t tokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": t.token}, nil
}
func (tokenCreds) RequireTransportSecurity() bool { return false } // insecure phase

// Client wraps srspb.SRSServiceClient with the per-call token.
type Client struct {
	rpc   srspb.SRSServiceClient
	token string
}

// New builds a control client bound to a token.
func New(conn grpc.ClientConnInterface, token string) *Client {
	return &Client{rpc: srspb.NewSRSServiceClient(conn), token: token}
}

func (c *Client) callOpts() []grpc.CallOption {
	return []grpc.CallOption{grpc.PerRPCCredentials(tokenCreds{c.token})}
}

// SyncClient fetches the initial snapshot and hydrates the store.
func (c *Client) SyncClient(ctx context.Context, st *state.Store) error {
	resp, err := c.rpc.SyncClient(ctx, &srspb.Empty{}, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("sync client: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("sync client: %s", resp.GetErrorMessage())
	}
	data := resp.GetData()
	for guid, ci := range data.GetClients() {
		st.UpdateClient(guid, ci)
	}
	for guid, ri := range data.GetRadios() {
		st.SetRadios(guid, ri)
	}
	if data.GetSettings() != nil {
		st.SetSettings(data.GetSettings())
	}
	return nil
}

// UpdateRadioInfo pushes the client's radio config to the server.
func (c *Client) UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error {
	resp, err := c.rpc.UpdateRadioInfo(ctx, info, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("update radio info: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("update radio info: %s", resp.GetErrorMessage())
	}
	return nil
}

// UpdateClientInfo pushes the client's metadata to the server.
func (c *Client) UpdateClientInfo(ctx context.Context, info *srspb.ClientInfo) error {
	resp, err := c.rpc.UpdateClientInfo(ctx, info, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("update client info: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("update client info: %s", resp.GetErrorMessage())
	}
	return nil
}

// GetServerSettings fetches current server settings.
func (c *Client) GetServerSettings(ctx context.Context) (*srspb.ServerSettings, error) {
	return c.rpc.GetServerSettings(ctx, &srspb.Empty{}, c.callOpts()...)
}

// Disconnect notifies the server the client is leaving.
func (c *Client) Disconnect(ctx context.Context) error {
	_, err := c.rpc.Disconnect(ctx, &srspb.Empty{}, c.callOpts()...)
	return err
}
