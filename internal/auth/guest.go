package auth

import (
	"context"
	"fmt"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// GuestResult is the outcome of a successful guest login.
type GuestResult struct {
	Token     string
	Coalition string
}

// GuestLogin authenticates via the guest path. A server-side rejection maps to
// ErrLoginRejected.
func (c *Client) GuestLogin(ctx context.Context, name, password, unitID, clientGUID string) (GuestResult, error) {
	resp, err := c.rpc.GuestLogin(ctx, &srspb.GuestLoginRequest{
		Name:       name,
		Password:   password,
		UnitId:     unitID,
		ClientGuid: clientGUID,
	})
	if err != nil {
		return GuestResult{}, fmt.Errorf("guest login: %w", err)
	}
	if !resp.GetSuccess() {
		return GuestResult{}, fmt.Errorf("%w: %s", ErrLoginRejected, resp.GetErrorMessage())
	}
	r := resp.GetResult()
	return GuestResult{Token: r.GetToken(), Coalition: r.GetCoalition()}, nil
}
