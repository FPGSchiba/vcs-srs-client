package control

import (
	"context"
	"time"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// PingOnce sends one Ping carrying the previously measured RTT and returns the
// RTT of this round-trip in milliseconds.
func (c *Client) PingOnce(ctx context.Context, lastRTTms int64) (int64, error) {
	start := time.Now()
	_, err := c.rpc.Ping(ctx, &srspb.PingRequest{LastRttMs: lastRTTms}, c.callOpts()...)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}
