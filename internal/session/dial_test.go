package session

import (
	"strings"
	"testing"
)

func TestInsecureDialer_Validation(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		wantErr   string // substring; "" means a dialer is returned
	}{
		{"empty", "", "empty"},
		{"no port", "localhost", "host:port"},
		{"remote rejected", "srs.example.org:5002", "non-local"},
		{"localhost ok", "localhost:5002", ""},
		{"127.0.0.1 ok", "127.0.0.1:5002", ""},
		{"ipv6 loopback ok", "[::1]:5002", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := insecureDialer(tc.serverURL)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected a dialer, got error: %v", err)
				}
				if d == nil {
					t.Fatal("expected non-nil dialer")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
