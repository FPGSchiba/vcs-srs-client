package loadgen

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Death causes (I-10).
const (
	CauseStream = "stream" // the update stream ended for a reason other than ResourceExhausted
	CauseResync = "resync" // a resync after ResourceExhausted failed
	CausePing   = "ping"   // consecutive Ping failures reached the threshold
)

// StreamEndedByServer is the message for a clean, error-free stream end.
const StreamEndedByServer = "update stream ended by the server"

// DeathRecord describes why a bot's control plane was declared dead. Every
// field is always encoded, including zero values: a reader must be able to
// tell "0 failed pings" from "field missing".
type DeathRecord struct {
	Bot              int     `json:"bot"`
	Name             string  `json:"name"`
	GUID             string  `json:"guid"`
	Cause            string  `json:"cause"`
	Code             string  `json:"code"`
	Message          string  `json:"message"`
	Error            string  `json:"error"`
	Phase            string  `json:"phase"`
	AtUnixMs         int64   `json:"at_unix_ms"`
	AgeS             float64 `json:"age_s"`
	SinceLastOKPingS float64 `json:"since_last_ok_ping_s"`
	PingFailures     int     `json:"ping_failures"`
	LocalAddr        string  `json:"local_addr"`
}

// DeathLine is the stdout line wrapper: a DeathRecord plus the line type and
// the process-wide dead count.
type DeathLine struct {
	Type string `json:"type"`
	DeathRecord
	DeadTotal int `json:"dead_total"`
}

// NewDeathRecord fills cause, code, message and error from err. A nil err is
// a clean server-side stream end (code OK). A non-status error is code
// Unknown with its own text as the message. A status error wrapped with
// context (fmt.Errorf("resync: %w", ...)) keeps its own code and message.
// The caller fills in identity, phase and timing; SinceLastOKPingS starts at
// -1 (unknown).
func NewDeathRecord(cause string, err error) DeathRecord {
	d := DeathRecord{Cause: cause, SinceLastOKPingS: -1}
	if err == nil {
		d.Code = codes.OK.String()
		d.Message = StreamEndedByServer
		d.Error = StreamEndedByServer
		return d
	}
	d.Error = err.Error()
	var gs interface{ GRPCStatus() *status.Status }
	if errors.As(err, &gs) {
		if st := gs.GRPCStatus(); st != nil {
			d.Code = st.Code().String()
			d.Message = st.Message()
			return d
		}
	}
	d.Code = codes.Unknown.String()
	d.Message = err.Error()
	return d
}
