// Command vcs-loadbot drives synthetic load against a LOCAL test server.
// It refuses any non-loopback or unlisted address before opening any socket.
package main

import (
	"context"
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
	fs.String("scenario", "", "scenario name")
	fs.String("arm", "S", "arm")
	fs.String("passwords", "", "coalition passwords JSON file")
	fs.String("unit", "LDBT", "unit label")
	fs.Int64("seed", 0, "seed")
	fs.String("bounds", "", "bounds")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errw, "vcs-loadbot: unexpected argument %q (all inputs are flags)
", fs.Arg(0))
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

	fmt.Fprintf(errw, "vcs-loadbot %s: not implemented\n", sub)
	return 1
}
