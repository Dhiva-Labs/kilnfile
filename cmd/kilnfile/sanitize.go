package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Dhiva-Labs/kilnfile/internal/policy"
	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
	"github.com/Dhiva-Labs/kilnfile/internal/worker"
)

const (
	exitOK       = 0
	exitError    = 1
	exitRejected = 2
)

type sanitizeOpts struct {
	input      string
	output     string
	policyPath string
	reportPath string
	workerPath string
	timeout    time.Duration
	force      bool
}

func parseSanitizeArgs(args []string, stderr io.Writer) (sanitizeOpts, error) {
	var o sanitizeOpts
	fl := flag.NewFlagSet("sanitize", flag.ContinueOnError)
	fl.SetOutput(stderr)
	fl.StringVar(&o.output, "o", "", "output file (required)")
	fl.StringVar(&o.output, "output", "", "output file (required)")
	fl.StringVar(&o.policyPath, "policy", "", "YAML policy file")
	fl.StringVar(&o.reportPath, "report", "", "write the JSON report here")
	fl.StringVar(&o.workerPath, "worker", "", "path to kilnfile-worker")
	fl.DurationVar(&o.timeout, "timeout", 30*time.Second, "worker time limit")
	fl.BoolVar(&o.force, "force", false, "overwrite an existing output file")

	// Accept flags before or after the positional input.
	var pos []string
	for {
		if err := fl.Parse(args); err != nil {
			return o, err
		}
		if fl.NArg() == 0 {
			break
		}
		pos = append(pos, fl.Arg(0))
		args = fl.Args()[1:]
	}
	if len(pos) != 1 {
		return o, errors.New("sanitize takes exactly one input file")
	}
	o.input = pos[0]
	if o.output == "" {
		return o, errors.New("-o <output> is required")
	}
	if o.timeout <= 0 {
		return o, errors.New("--timeout must be positive")
	}
	return o, nil
}

func runSanitize(ctx context.Context, args []string, stderr io.Writer) int {
	o, err := parseSanitizeArgs(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "kilnfile: %v\n\n%s", err, usage)
		}
		return exitError
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "kilnfile: %v\n", err)
		return exitError
	}

	cfg := policy.Default()
	if o.policyPath != "" {
		if cfg, err = policy.LoadFile(o.policyPath); err != nil {
			return fail(err)
		}
	}
	if err := checkPaths(o); err != nil {
		return fail(err)
	}
	workerPath, err := worker.Locate(o.workerPath)
	if err != nil {
		return fail(err)
	}

	payload, tooBig, err := readInput(o.input, cfg.Limits.MaxInputBytes)
	if err != nil {
		return fail(err)
	}

	var (
		rep    protocol.Report
		out    []byte
		runErr error
	)
	if tooBig {
		rep = protocol.Rejected("limit_exceeded")
		rep.LimitsHit = []string{"max_input_bytes"}
		runErr = fmt.Errorf("input is larger than max_input_bytes (%d)", cfg.Limits.MaxInputBytes)
	} else {
		r := &worker.Runner{Path: workerPath, Timeout: o.timeout}
		rep, out, runErr = r.Run(ctx, cfg.Header(), payload)
	}

	if o.reportPath != "" {
		if err := writeReport(o.reportPath, rep); err != nil {
			return fail(err)
		}
	}

	if rep.Result == protocol.ResultRejected {
		fmt.Fprintf(stderr, "kilnfile: rejected %s: reason=%s\n", o.input, rep.Reason)
		if runErr != nil {
			fmt.Fprintf(stderr, "kilnfile: %v\n", runErr)
		}
		return exitRejected
	}

	if err := writeOutput(o.output, out, o.force); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stderr, "kilnfile: %s %s -> %s (%s -> %s, %d bytes, %d actions)\n",
		rep.Result, o.input, o.output, inputType(rep), rep.Output.Type, len(out), len(rep.Actions))
	return exitOK
}

// checkPaths fails early on output problems, before any work is done.
func checkPaths(o sanitizeOpts) error {
	in, err := os.Stat(o.input)
	if err != nil {
		return err
	}
	if !in.Mode().IsRegular() {
		return fmt.Errorf("input %s is not a regular file", o.input)
	}
	st, err := os.Lstat(o.output)
	switch {
	case err == nil:
		if os.SameFile(in, st) {
			return errors.New("output is the same file as the input")
		}
		if !o.force {
			return fmt.Errorf("output %s exists (use --force to overwrite)", o.output)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return nil
}

// readInput reads the file up to max bytes. tooBig is true when it is longer.
func readInput(path string, max uint64) (data []byte, tooBig bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, false, err
	}
	if uint64(len(data)) > max {
		return nil, true, nil
	}
	return data, false, nil
}

// writeAtomic writes data to a temp file beside path and renames it into
// place. With noClobber it never replaces an existing file.
func writeAtomic(path string, data []byte, noClobber bool) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kilnfile-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	_ = tmp.Chmod(0o644) // unsupported on some platforms; the default mode is fine
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if noClobber {
		// Link fails if the target exists, closing the check-then-rename race.
		err := os.Link(tmpName, path)
		if err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("output %s exists (use --force to overwrite)", path)
		}
		// Hard links unsupported here: fall back to a checked rename.
		if _, serr := os.Lstat(path); serr == nil {
			return fmt.Errorf("output %s exists (use --force to overwrite)", path)
		}
	}
	return os.Rename(tmpName, path)
}

func writeOutput(path string, data []byte, force bool) error {
	return writeAtomic(path, data, !force)
}

func writeReport(path string, rep protocol.Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), false)
}

func inputType(rep protocol.Report) string {
	if rep.Input == nil || rep.Input.DetectedType == "" {
		return "unknown"
	}
	return rep.Input.DetectedType
}
