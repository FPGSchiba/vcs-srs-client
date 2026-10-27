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

// RequireTransportSecurity is false because the loopback dial can still be
// plaintext by design (see internal/session/dial.go and spec §5.1); returning
// true here would break that dev path. Remote dials are TLS-only regardless.
func (tokenCreds) RequireTransportSecurity() bool { return false }

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

// SyncClient fetches the snapshot and hydrates the store with it.
//
// The snapshot REPLACES the roster: st.ResetRoster runs immediately before
// hydrating, and only on a successful response, so a failed call leaves the
// roster as it was and a successful one leaves no ghosts.
//
// An empty voice_secret never overwrites a stored one -- the same rule
// stream.go applies to VOICE_ADDRESS_UPDATE. The addresses are still updated
// (a standalone server legitimately returns "" for both). hadSecret reports
// whether the response carried a secret, so a caller that expects one (a
// Reconnect) can treat its absence as the session being gone.
func (c *Client) SyncClient(ctx context.Context, st *state.Store) (hadSecret bool, err error) {
	resp, err := c.rpc.SyncClient(ctx, &srspb.Empty{}, c.callOpts()...)
	if err != nil {
		return false, fmt.Errorf("sync client: %w", err)
	}
	if !resp.GetSuccess() {
		return false, fmt.Errorf("sync client: %s", resp.GetErrorMessage())
	}
	data := resp.GetData()
	st.ResetRoster()
	for guid, ci := range data.GetClients() {
		st.UpdateClient(guid, ci)
	}
	for guid, ri := range data.GetRadios() {
		st.SetRadios(guid, ri)
	}
	if data.GetSettings() != nil {
		st.SetSettings(data.GetSettings())
	}
	// coalition_voice_addr and global_voice_addr are "" on a standalone
	// server (no registry to populate them from) and that is stored as-is --
	// see Store.SetVoiceCredentials.
	if secret := data.GetVoiceSecret(); secret != "" {
		st.SetVoiceCredentials(secret, data.GetCoalitionVoiceAddr(), data.GetGlobalVoiceAddr())
		return true, nil
	}
	st.SetVoiceAddresses(data.GetCoalitionVoiceAddr(), data.GetGlobalVoiceAddr())
	return false, nil
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

// Disconnect notifies the server the client is leaving.
func (c *Client) Disconnect(ctx context.Context) error {
	_, err := c.rpc.Disconnect(ctx, &srspb.Empty{}, c.callOpts()...)
	return err
}
