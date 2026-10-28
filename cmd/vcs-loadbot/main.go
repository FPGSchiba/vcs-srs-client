// Command vcs-loadbot drives synthetic load against a LOCAL test server.
// It refuses any non-loopback or unlisted address before opening any socket.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// addrFlags names the target address flags checked for each subcommand.
var addrFlags = []string{"control", "voice", "reflector"}

func run(ctx context.Context, args []string, in io.Reader, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, "usage: vcs-loadbot run|reflect|calibrate [flags]")
		return 2
	}
	sub := args[0]
	switch sub {
	case "run", "reflect", "calibrate":
	default:
		fmt.Fprintf(errw, "unknown subcommand %q\n", sub)
		return 2
	}

	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(errw)
	vals := map[string]*string{}
	for _, n := range addrFlags {
		vals[n] = fs.String(n, "", "target "+n+" address (loopback IP literal host:port)")
	}
	allow := fs.String("allow", "", "comma-separated allowed loopback host:port list")
	scenario := fs.String("scenario", "", "scenario name or file")
	arm := fs.String("arm", "S", "arm (S or Seq)")
	passwords := fs.String("passwords", "", "coalition passwords JSON file ({coalition: password})")
	unit := fs.String("unit", "LDBT", "unit label")
	seed := fs.Int64("seed", 0, "seed")
	bounds := fs.String("bounds", "", "JSON overrides for the generator self-health bounds")
	listen := fs.String("listen", "127.0.0.1:0", "reflect: loopback address to listen on (port 0 = ephemeral)")
	senders := fs.Int("senders", 2, "reflect: sender goroutines on the one socket (>= 2)")
	steps := fs.String("steps", "64,128,256,512", "calibrate: ascending bot counts")
	hold := fs.Float64("hold", 3, "calibrate: seconds per step")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errw, "vcs-loadbot: unexpected argument %q (all inputs are flags)\n", fs.Arg(0))
		return 2
	}

	al, err := loadgen.ParseAllow(*allow)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: --allow: %v\n", err)
		return 2
	}
	// Every address check precedes any socket or grpc.NewClient.
	for _, n := range addrFlags {
		v := *vals[n]
		if v == "" {
			continue
		}
		if err := al.Check(v); err != nil {
			fmt.Fprintf(errw, "vcs-loadbot: --%s refused: %v\n", n, err)
			return 2
		}
	}

	switch sub {
	case "reflect":
		// The listen port may be 0, so it cannot be an allow-list entry; it
		// must still be a loopback IP literal.
		if l, err := loadgen.ParseAllow(*listen); err != nil || len(l) != 1 {
			if err == nil {
				err = loadgen.ErrNotLoopback
			}
			fmt.Fprintf(errw, "vcs-loadbot: --listen refused: %v\n", err)
			return 2
		}
		return reflectCmd(ctx, *listen, *senders, in, out, errw)
	case "calibrate":
		return calibrateCmd(ctx, *vals["reflector"], *steps, *hold, *seed, *bounds, al, out, errw)
	}
	return runCmd(ctx, runOpts{
		scenario: *scenario, arm: *arm, control: *vals["control"], voice: *vals["voice"],
		passwords: *passwords, unit: *unit, seed: *seed, bounds: *bounds, allow: al,
	}, in, out, errw)
}

type runOpts struct {
	scenario, arm, control, voice, passwords, unit, bounds string
	seed                                                   int64
	allow                                                  loadgen.AllowList
}

// runCmd implements `vcs-loadbot run`: persistent bots driven by NDJSON on
// stdin, step reports on stdout. Addresses were checked by the caller.
func runCmd(ctx context.Context, o runOpts, in io.Reader, out, errw io.Writer) int {
	switch o.arm {
	case "S", "Seq":
	case "D2":
		fmt.Fprintln(errw, "vcs-loadbot: D2 arrives in B-1 (W6 C2)")
		return 2
	default:
		fmt.Fprintf(errw, "vcs-loadbot: unknown --arm %q (S or Seq)\n", o.arm)
		return 2
	}
	if o.control == "" || o.scenario == "" || o.passwords == "" {
		fmt.Fprintln(errw, "vcs-loadbot: run needs --control, --scenario and --passwords")
		return 2
	}
	sc, err := loadgen.LoadScenario(o.scenario)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 2
	}
	raw, err := os.ReadFile(o.passwords)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: --passwords: %v\n", err)
		return 2
	}
	var pw map[string]string
	if err := json.Unmarshal(raw, &pw); err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: --passwords: %v\n", err)
		return 2
	}
	b, err := loadgen.ParseBounds(o.bounds)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: --bounds: %v\n", err)
		return 2
	}
	r, err := loadgen.NewRunner(loadgen.RunConfig{
		Arm: o.arm, Scenario: sc, ControlAddr: o.control, VoiceAddr: o.voice,
		Allow: o.allow, Passwords: pw, Unit: o.unit, Seed: o.seed, Bounds: b,
	}, loadgen.DialInsecure(o.control))
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 1
	}
	if err := r.Serve(ctx, in, out); err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 1
	}
	return 0
}

// reflectCmd runs the calibration reflector until stdin EOF, {"quit":true}
// or ctx is done. Its first stdout line is {"type":"ready","addr":...}; its
// last is a stats line carrying the send-error count, which the kit combines
// with the calibration result (the two run in different processes).
func reflectCmd(ctx context.Context, listen string, senders int, in io.Reader, out, errw io.Writer) int {
	r, err := loadgen.NewReflector(listen, senders)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 1
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- r.Serve(rctx) }()
	writeJSON(out, map[string]any{"type": "ready", "addr": r.Addr()})
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			if c, err := loadgen.ParseCommand(sc.Bytes()); err == nil && c.Quit {
				break
			}
		}
		cancel()
	}()
	err = <-served
	writeJSON(out, reflectorStatsLine{Type: "reflector_stats", SendErrors: r.SendErrors(),
		QueueFullDrops: r.QueueFullDrops(), WriteErrors: r.WriteErrors(), Senders: senders})
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: reflector: %v\n", err)
		return 1
	}
	return 0
}

func parseSteps(csv string) ([]int, error) {
	var ns []int
	for _, e := range strings.Split(csv, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(e))
		if err != nil || n < 2 {
			return nil, fmt.Errorf("--steps: %q is not an integer >= 2", e)
		}
		if len(ns) > 0 && n <= ns[len(ns)-1] {
			return nil, fmt.Errorf("--steps: %d is not ascending", n)
		}
		ns = append(ns, n)
	}
	return ns, nil
}

func calibrateCmd(ctx context.Context, refl, stepsCSV string, holdS float64, seed int64, boundsJSON string, al loadgen.AllowList, out, errw io.Writer) int {
	if refl == "" {
		fmt.Fprintln(errw, "vcs-loadbot: calibrate needs --reflector")
		return 2
	}
	ns, err := parseSteps(stepsCSV)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 2
	}
	if !(holdS > 0) {
		fmt.Fprintln(errw, "vcs-loadbot: --hold must be > 0")
		return 2
	}
	b, err := loadgen.ParseBounds(boundsJSON)
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: --bounds: %v\n", err)
		return 2
	}
	var mu sync.Mutex // the step hook and the progress goroutine share out
	emit := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		writeJSON(out, v)
	}
	br, bo := loadgen.NewBoundsReport(b), loadgen.ReportLabel
	steps, capy, err := loadgen.Calibrate(ctx, refl, al, seed, ns, time.Duration(holdS*float64(time.Second)),
		loadgen.WithBounds(b),
		loadgen.WithProgress(5*time.Second, func(p loadgen.ProgressLine) { emit(p) }),
		loadgen.WithStepHook(func(s loadgen.CalStep) {
			emit(calibrationLine{
				Type: "calibration", N: s.N, Bots: s.Bots,
				EgressPPS: s.EgressPPS, ExpectedPPS: s.ExpectedPPS,
				Valid: s.Valid, Violations: s.Violations,
				HelloTriesMax: s.HelloTriesMax, HelloRetried: s.HelloRetried,
				Health: loadgen.NewHealthReport(s.Health), Bounds: br,
				Label: bo, Payload: loadgen.PayloadLabel,
			})
		}))
	var stop *loadgen.CalStoppedError
	if errors.As(err, &stop) {
		emit(calibrationStoppedLine{
			Type: "calibration_stopped", N: stop.N, Bots: stop.Bots,
			Stage: stop.Stage, Bot: stop.Bot, Tries: stop.Tries,
			Reason: stop.Err.Error(),
		})
		if len(steps) == 0 {
			fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
			return 1
		}
		err = nil // the completed steps are the result
	}
	if err != nil {
		fmt.Fprintf(errw, "vcs-loadbot: %v\n", err)
		return 1
	}
	emit(receiveCapacityLine{
		Type: "receive_capacity", PPS: capy.PPS, LowerBound: capy.LowerBound,
		Label: loadgen.ReportLabel,
	})
	return 0
}

// The calibration lines are structs, not maps, so "type" leads every line
// (encoding/json would sort a map's keys alphabetically) and a raw Actions
// log reads like the I-7 fixture. Tests compare key sets, not order.
type calibrationLine struct {
	Type          string               `json:"type"`
	N             int                  `json:"n"`
	Bots          int                  `json:"bots"`
	EgressPPS     float64              `json:"egress_pps"`
	ExpectedPPS   float64              `json:"expected_pps"`
	Valid         bool                 `json:"valid"`
	Violations    []string             `json:"violations"`
	HelloTriesMax int                  `json:"hello_tries_max"`
	HelloRetried  int                  `json:"hello_retried"`
	Health        loadgen.HealthReport `json:"health"`
	Bounds        loadgen.BoundsReport `json:"bounds"`
	Label         string               `json:"label"`
	Payload       string               `json:"payload"`
}

type calibrationStoppedLine struct {
	Type   string `json:"type"`
	N      int    `json:"n"`
	Bots   int    `json:"bots"`
	Stage  string `json:"stage"`
	Bot    int    `json:"bot"`
	Tries  int    `json:"tries"`
	Reason string `json:"reason"`
}

// reflectorStatsLine is the reflect subcommand's last line (interface I-5).
// SendErrors = QueueFullDrops + WriteErrors, its v1 meaning.
type reflectorStatsLine struct {
	Type           string `json:"type"`
	SendErrors     uint64 `json:"send_errors"`
	QueueFullDrops uint64 `json:"queue_full_drops"`
	WriteErrors    uint64 `json:"write_errors"`
	Senders        int    `json:"senders"`
}

type receiveCapacityLine struct {
	Type       string  `json:"type"`
	PPS        float64 `json:"pps"`
	LowerBound bool    `json:"lower_bound"`
	Label      string  `json:"label"`
}

func writeJSON(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = w.Write(append(b, '\n'))
}
