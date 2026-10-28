// Command vcs-loadbot drives synthetic load against a LOCAL test server.
// It refuses any non-loopback or unlisted address before opening any socket.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

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

	if sub != "run" {
		fmt.Fprintf(errw, "vcs-loadbot %s: not implemented\n", sub)
		return 1
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
