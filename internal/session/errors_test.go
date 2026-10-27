package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyRPCError(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("sync client: %w", err) }
	cases := []struct {
		name   string
		in     error
		want   verdict
		reason string
	}{
		{"unauthenticated unknown session", wrap(status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/SyncClient: log in again")), verdictSessionEnded, "expired"},
		{"unauthenticated kicked", wrap(status.Error(codes.Unauthenticated, "session ended: kicked")), verdictSessionEnded, "kicked"},
		{"unauthenticated stream lost", wrap(status.Error(codes.Unauthenticated, "session ended: stream lost")), verdictSessionEnded, "stream lost"},
		{"permission denied", wrap(status.Error(codes.PermissionDenied, "insufficient role for x")), verdictSessionEnded, "expired"},
		{"internal", wrap(status.Error(codes.Internal, "token verification unavailable")), verdictOrdinary, ""},
		{"aborted", wrap(status.Error(codes.Aborted, "superseded by a newer subscription")), verdictSuperseded, ""},
		{"resource exhausted", wrap(status.Error(codes.ResourceExhausted, "update stream fell behind: resubscribe and call SyncClient")), verdictResync, ""},
		{"unavailable", wrap(status.Error(codes.Unavailable, "x")), verdictOrdinary, ""},
		{"plain", errors.New("plain"), verdictOrdinary, ""},
		{"nil", nil, verdictOrdinary, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, reason := classifyRPCError(tc.in)
			if v != tc.want || reason != tc.reason {
				t.Fatalf("classifyRPCError = (%v, %q), want (%v, %q)", v, reason, tc.want, tc.reason)
			}
		})
	}
}

func TestServerNotRespondingError(t *testing.T) {
	e := error(&serverNotRespondingError{after: 15 * time.Second, cause: fmt.Errorf("init auth: %w", context.DeadlineExceeded)})
	if !errors.Is(e, ErrServerNotResponding) {
		t.Fatal("errors.Is(e, ErrServerNotResponding) = false")
	}
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("errors.Is(e, context.DeadlineExceeded) = false; the cause must unwrap")
	}
	want := "The server accepted the connection but did not answer within 15 s. Check the address, or try again later."
	if e.Error() != want {
		t.Fatalf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestSessionEndedError(t *testing.T) {
	err := sessionEndedError("kicked")
	if !errors.Is(err, ErrSessionEnded) {
		t.Fatal("sessionEndedError must wrap ErrSessionEnded")
	}
	if err.Error() != "session ended: kicked" {
		t.Fatalf("Error() = %q", err.Error())
	}
}
