package voice

import (
	"net"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

// These tests live here, not under testdata/, because `./...` never runs
// _test.go files inside a directory named "testdata".

func TestFreeUDPPortIsUDPBindable(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := testdata.FreeUDPPort(t)
		if p <= 0 || p >= 65536 {
			t.Fatalf("iteration %d: port %d out of range", i, p)
		}
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		if err != nil {
			t.Fatalf("iteration %d: UDP port %d not bindable: %v", i, p, err)
		}
		c.Close()
	}
}

func TestIsBindError(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want bool
	}{
		{"windows-reserved", `level=ERROR msg="Failed to start voice server" error="listen udp 127.0.0.1:54600: bind: An attempt was made to access a socket in a way forbidden by its access permissions."`, true},
		{"windows-in-use", `level=ERROR msg="Failed to start voice server" error="listen udp 127.0.0.1:54600: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted."`, true},
		{"unix-in-use", `level=ERROR msg="Failed to start voice server" error="listen udp 127.0.0.1:54600: bind: address already in use"`, true},
		{"config-error", `level=ERROR msg="Failed to load config" error="yaml: line 3: mapping values are not allowed"`, false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := testdata.IsBindError(c.log); got != c.want {
				t.Fatalf("IsBindError = %v, want %v", got, c.want)
			}
		})
	}
}
