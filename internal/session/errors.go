package session

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrConnectCancelled is returned by Connect/Reconnect when CancelConnect (or
// a Disconnect, which cancels first) aborted the attempt. The frontend shows
// no error for it.
var ErrConnectCancelled = errors.New("connect cancelled")

// ErrServerNotResponding is matched (errors.Is) by the error Connect and
// Reconnect return when the post-dial sequence did not finish within its
// bound: the server accepted the connection and then never answered.
var ErrServerNotResponding = errors.New("server not responding")

// ErrSessionEnded is wrapped by every error returned because the server ended
// this client's session (removed, kicked, banned, restarted, token expired).
// By the time it is returned the session has already been torn down and
// auth:session_ended + auth:session_changed=logged_out emitted.
var ErrSessionEnded = errors.New("session ended")

// serverNotRespondingError is the user-facing timeout error. Its text is the
// copy the Welcome screen shows verbatim.
type serverNotRespondingError struct {
	after time.Duration
	cause error
}

func (e *serverNotRespondingError) Error() string {
	return fmt.Sprintf("The server accepted the connection but did not answer within %d s. Check the address, or try again later.",
		int(e.after/time.Second))
}

func (e *serverNotRespondingError) Is(target error) bool { return target == ErrServerNotResponding }

func (e *serverNotRespondingError) Unwrap() error { return e.cause }

// sessionEndedError builds the error returned once endSession has run.
func sessionEndedError(reason string) error {
	return fmt.Errorf("%w: %s", ErrSessionEnded, reason)
}

// verdict is what an RPC failure means for the session.
type verdict int

const (
	// verdictOrdinary: a transport or server failure; the session may still
	// exist, so the token is kept and RECONNECT stays possible.
	verdictOrdinary verdict = iota
	// verdictSessionEnded: the server no longer knows this session.
	verdictSessionEnded
	// verdictSuperseded: another subscriber took over this session's update
	// stream (codes.Aborted).
	verdictSuperseded
	// verdictResync: the update stream fell behind (codes.ResourceExhausted);
	// resubscribe and SyncClient on the same connection.
	verdictResync
)

func (v verdict) String() string {
	switch v {
	case verdictSessionEnded:
		return "session-ended"
	case verdictSuperseded:
		return "superseded"
	case verdictResync:
		return "resync"
	}
	return "ordinary"
}

// sessionEndedPrefix precedes the reason in the server's stream-end message
// ("session ended: kicked").
const sessionEndedPrefix = "session ended: "

// classifyRPCError decides what err means for the session. It branches on the
// gRPC status code only (status.Code unwraps %w chains); the message text is
// read solely to extract the reason suffix used for the user-facing copy, so
// a server rewording costs copy, not behaviour. reason is "" unless the
// verdict is verdictSessionEnded, where it falls back to "expired".
func classifyRPCError(err error) (v verdict, reason string) {
	if err == nil {
		return verdictOrdinary, ""
	}
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		// For a wrapped status, Convert's message is the whole err.Error()
		// ("sync client: rpc error: ... desc = session ended: kicked"); the
		// reason is whatever follows the prefix either way.
		msg := status.Convert(err).Message()
		if i := strings.Index(msg, sessionEndedPrefix); i >= 0 {
			if r := strings.TrimSpace(msg[i+len(sessionEndedPrefix):]); r != "" {
				return verdictSessionEnded, r
			}
		}
		return verdictSessionEnded, "expired"
	case codes.Aborted:
		return verdictSuperseded, ""
	case codes.ResourceExhausted:
		return verdictResync, ""
	}
	return verdictOrdinary, ""
}
