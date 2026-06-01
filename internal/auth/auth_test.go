package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

func TestInitAuth_ReturnsGUIDAndGuestFlag(t *testing.T) {
	f := grpctest.NewFake()
	f.ClientGUID = "g-1"
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	conn, err := dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := auth.New(conn)

	res, err := c.InitAuth(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("InitAuth: %v", err)
	}
	if res.ClientGUID != "g-1" || !res.HasGuest {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestGuestLogin_Success(t *testing.T) {
	f := grpctest.NewFake()
	f.GuestToken, f.GuestCoalition = "tok", "VG"
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := auth.New(conn)

	out, err := c.GuestLogin(context.Background(), "Name", "pw", "unit", "g-1")
	if err != nil {
		t.Fatalf("GuestLogin: %v", err)
	}
	if out.Token != "tok" || out.Coalition != "VG" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestGuestLogin_Rejected(t *testing.T) {
	f := grpctest.NewFake()
	f.RejectLogin = true
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := auth.New(conn)

	_, err := c.GuestLogin(context.Background(), "n", "bad", "u", "g")
	if !errors.Is(err, auth.ErrLoginRejected) {
		t.Fatalf("expected ErrLoginRejected, got %v", err)
	}
}
