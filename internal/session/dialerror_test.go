package session

import (
	"errors"
	"strings"
	"testing"
)

func TestExplainDialError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		serverURL string
		caFile    string
		want      []string // all must appear in the message
	}{
		{
			name:      "plaintext server",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: tls: first record does not look like a TLS handshake"`),
			serverURL: "srs.example.org:5002",
			want:      []string{"srs.example.org:5002", "not speaking TLS", "clientTLS"},
		},
		{
			// Review Focus 5: the first failure a self-hoster hits. The
			// message has to name the remedy, not just the symptom.
			name:      "untrusted issuer, no pin configured",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate signed by unknown authority"`),
			serverURL: "srs.example.org:5002",
			want:      []string{"srs.example.org:5002", "tls_ca_file"},
		},
		{
			name:      "untrusted issuer with a pin configured",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate signed by unknown authority"`),
			serverURL: "srs.example.org:5002",
			caFile:    "/certs/ca.pem",
			want:      []string{"/certs/ca.pem", "not issued by"},
		},
		{
			name:      "hostname mismatch",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate is valid for srs.example.org, not 203.0.113.10"`),
			serverURL: "203.0.113.10:5002",
			want:      []string{"203.0.113.10:5002", "different name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := explainDialError(tc.err, tc.serverURL, tc.caFile)
			for _, want := range tc.want {
				if !strings.Contains(got.Error(), want) {
					t.Fatalf("message %q does not contain %q", got.Error(), want)
				}
			}
			// The original must stay reachable: the wrapped text is for the
			// user, the cause is for the log.
			if !errors.Is(got, tc.err) {
				t.Fatal("expected the original error to remain unwrappable via errors.Is")
			}
		})
	}
}

func TestExplainDialError_UnrecognisedErrorPassesThrough(t *testing.T) {
	// The substring matching below is the only signal available, so an
	// unrecognised failure must degrade to the raw error rather than being
	// swallowed or mislabelled.
	orig := errors.New("connection refused")
	got := explainDialError(orig, "srs.example.org:5002", "")
	if got != orig {
		t.Fatalf("expected the original error unchanged, got %v", got)
	}
}
