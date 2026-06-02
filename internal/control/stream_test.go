package control_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

type capEmitter struct {
	mu    sync.Mutex
	names []string
}

func (c *capEmitter) Emit(name string, _ any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, name)
}
func (c *capEmitter) saw(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.names {
		if n == name {
			return true
		}
	}
	return false
}

func TestStream_RoutesClientJoined(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	em := &capEmitter{}
	c := control.New(conn, "tok")

	guid := "g9"
	f.PushUpdate(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_CLIENT_JOINED,
		Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{
			ClientGuid: &guid,
			ClientInfo: &srspb.ClientInfo{Name: "Zoe"},
		}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.ConsumeUpdates(ctx, st, em)

	deadline := time.After(2 * time.Second)
	for {
		if _, ok := st.Client(guid); ok && em.saw("state:client_update") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("client_update not observed in time")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
