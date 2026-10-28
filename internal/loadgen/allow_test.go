package loadgen

import (
	"errors"
	"testing"
)

func TestAllowListCheck(t *testing.T) {
	only2 := mustAllow(t, "127.0.0.1:5002")
	both := mustAllow(t, "127.0.0.1:5002,[::1]:5002")

	rows := []struct {
		name string
		al   AllowList
		addr string
		want error
	}{
		{"v4 listed", only2, "127.0.0.1:5002", nil},
		{"v6 listed", both, "[::1]:5002", nil},
		{"private ip", only2, "10.0.0.1:5002", ErrNotLoopback},
		{"localhost name", only2, "localhost:5002", ErrNotLoopback},
		{"unspecified", only2, "0.0.0.0:5002", ErrNotLoopback},
		{"hostname", only2, "example.com:1", ErrNotLoopback},
		{"wrong port", only2, "127.0.0.1:5003", ErrNotAllowed},
		{"v6 unlisted", only2, "[::1]:5002", ErrNotAllowed},
		{"v4-mapped v6 not equal to listed v4", only2, "[::ffff:127.0.0.1]:5002", ErrNotAllowed},
		{"v6 loopback with zone not equal to listed", both, "[::1%eth0]:5002", ErrNotAllowed},
		{"non-loopback v6 with zone", both, "[fe80::1%eth0]:5002", ErrNotLoopback},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			err := r.al.Check(r.addr)
			if !errors.Is(err, r.want) {
				t.Fatalf("Check(%q) = %v, want %v", r.addr, err, r.want)
			}
		})
	}

	if _, err := ParseAllow("10.0.0.1:1"); err == nil {
		t.Fatal("ParseAllow of a non-loopback entry must error")
	}
	if _, err := ParseAllow("localhost:1"); err == nil {
		t.Fatal("ParseAllow of a hostname must error")
	}
}

func mustAllow(t *testing.T, csv string) AllowList {
	t.Helper()
	al, err := ParseAllow(csv)
	if err != nil {
		t.Fatalf("ParseAllow(%q): %v", csv, err)
	}
	return al
}
