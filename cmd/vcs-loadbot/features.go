package main

import (
	"fmt"
	"io"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen"
)

// defaultPingEvery is the --ping-every default and the features line's
// ping_every_s, so the two cannot drift.
const defaultPingEvery = 5 * time.Second

// featuresLine is the I-8 line. Structs keep "type" first; tests compare
// the decoded map. The kit reads it to detect which v3 flags and commands
// this bot supports.
type featuresLine struct {
	Type     string           `json:"type"`
	Schema   string           `json:"schema"`
	Features []string         `json:"features"` // sorted ascending
	Defaults featuresDefaults `json:"defaults"`
}

type featuresDefaults struct {
	PingEveryS       float64 `json:"ping_every_s"`
	JoinRateMax      float64 `json:"join_rate_max"`
	JoinRateScale    float64 `json:"join_rate_scale"`
	MaxInFlight      int     `json:"max_in_flight"`
	RampTalker       string  `json:"ramp_talker"`
	GCPauseP99US     int64   `json:"gc_pause_p99_us"`
	GCPauseWarnP99US int64   `json:"gc_pause_warn_p99_us"`
}

func newFeaturesLine() featuresLine {
	b := loadgen.DefaultBounds
	return featuresLine{
		Type:     "features",
		Schema:   "loadbot-features/1",
		Features: []string{"burst", "control_lost", "gc_warn", "join_rate", "ping", "talker_pause"},
		Defaults: featuresDefaults{
			PingEveryS:       defaultPingEvery.Seconds(),
			JoinRateMax:      loadgen.DefaultJoinRateMax,
			JoinRateScale:    loadgen.DefaultJoinRateScale,
			MaxInFlight:      loadgen.DefaultMaxInFlight,
			RampTalker:       loadgen.RampTalkerSilent,
			GCPauseP99US:     b.GCPauseP99.Microseconds(),
			GCPauseWarnP99US: b.GCPauseWarnP99.Microseconds(),
		},
	}
}

// featuresCmd prints the one features line. It takes no flags and opens no
// socket.
func featuresCmd(args []string, out, errw io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(errw, "vcs-loadbot: features takes no arguments (got %q)\n", args[0])
		return 2
	}
	writeJSON(out, newFeaturesLine())
	return 0
}
