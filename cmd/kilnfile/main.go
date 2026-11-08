// Command kilnfile is the front end for the kilnfile CDR engine.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// Version is the kilnfile CLI version.
const Version = "0.1.0"

const usage = `usage:
  kilnfile sanitize <input> -o <output> [--policy p.yaml] [--report r.json]
                    [--worker PATH] [--timeout 30s] [--force]
  kilnfile version

exit codes: 0 sanitized or unchanged, 2 rejected, 1 usage or internal error
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitError
	}
	switch args[0] {
	case "sanitize":
		return runSanitize(ctx, args[1:], stderr)
	case "version", "--version":
		fmt.Fprintf(stdout, "kilnfile %s (worker protocol 1)\n", Version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "kilnfile: unknown command %q\n\n%s", args[0], usage)
		return exitError
	}
}
