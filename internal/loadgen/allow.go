// Package loadgen is the load-bot engine. It is loopback-only by construction:
// every target address must pass AllowList.Check before any socket is opened.
package loadgen

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

var (
	// ErrNotLoopback means the host is not a loopback IP literal (hostnames,
	// including "localhost", are rejected so no resolver can redirect us).
	ErrNotLoopback = errors.New("address is not loopback")
	// ErrNotAllowed means the address is loopback but not in the allow list.
	ErrNotAllowed = errors.New("address is not in the allow list")
)

// AllowList is the set of exact host:port endpoints the bot may touch.
type AllowList map[string]struct{}

func parseLoopback(hostport string) (netip.AddrPort, error) {
	ap, err := netip.ParseAddrPort(hostport)
	if err != nil || !ap.Addr().IsLoopback() {
		return netip.AddrPort{}, fmt.Errorf("%q: %w", hostport, ErrNotLoopback)
	}
	return ap, nil
}

// ParseAllow parses a comma-separated host:port list. Every entry must be a
// loopback IP literal with a port.
func ParseAllow(csv string) (AllowList, error) {
	al := AllowList{}
	for _, e := range strings.Split(csv, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		ap, err := parseLoopback(e)
		if err != nil {
			return nil, err
		}
		al[ap.String()] = struct{}{}
	}
	return al, nil
}

// Check returns ErrNotLoopback or ErrNotAllowed (wrapped) unless hostport is a
// loopback IP literal listed exactly.
func (a AllowList) Check(hostport string) error {
	ap, err := parseLoopback(hostport)
	if err != nil {
		return err
	}
	if _, ok := a[ap.String()]; !ok {
		return fmt.Errorf("%q: %w", hostport, ErrNotAllowed)
	}
	return nil
}
