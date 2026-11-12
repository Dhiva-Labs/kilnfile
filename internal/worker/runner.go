// Package worker runs the sandboxed kilnfile-worker process for one file.
package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

// StderrCap is the most worker stderr output that is kept.
const StderrCap = 64 << 10

// Runner starts one worker process per file.
type Runner struct {
	// Path is the worker executable.
	Path string
	// Args are the worker arguments. Defaults to "--protocol 1".
	Args []string
	// Timeout bounds one run. Zero means no limit besides the context.
	Timeout time.Duration
	// Env is extra environment, in KEY=VALUE form, added to the current one.
	Env []string
}

// Failure is the error type for every worker failure. It always accompanies a
// Report with result rejected and reason worker_failure.
type Failure struct {
	Detail string
	Stderr string
	Err    error
}

func (f *Failure) Error() string {
	s := "worker failure: " + f.Detail
	if f.Stderr != "" {
		s += " (stderr: " + f.Stderr + ")"
	}
	return s
}

func (f *Failure) Unwrap() error { return f.Err }

// boundedBuffer keeps the first cap bytes written and discards the rest.
type boundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	cap int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.cap - b.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.buf.Write(p[:room])
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func snippet(s string) string {
	const n = 512
	if len(s) > n {
		s = s[:n] + "..."
	}
	return s
}

// Run sends one file to a fresh worker and returns its validated report and
// output. On any failure it returns a rejected/worker_failure report and a
// non-nil error (a *Failure).
func (r *Runner) Run(ctx context.Context, h protocol.Header, payload []byte) (protocol.Report, []byte, error) {
	fail := func(stderr string, err error, format string, a ...any) (protocol.Report, []byte, error) {
		return protocol.Rejected(protocol.ReasonWorkerFail), nil,
			&Failure{Detail: fmt.Sprintf(format, a...), Stderr: snippet(stderr), Err: err}
	}

	if r.Path == "" {
		return fail("", nil, "no worker path configured")
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return fail("", err, "not started: %v", err)
	}

	args := r.Args
	if args == nil {
		args = []string{"--protocol", "1"}
	}
	cmd := exec.Command(r.Path, args...)
	cmd.Env = append(os.Environ(), r.Env...)
	setProcessGroup(cmd)
	stderr := &boundedBuffer{cap: StderrCap}
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail("", err, "stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail("", err, "stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return fail("", err, "start %s: %v", r.Path, err)
	}

	// Kill the whole process group on timeout or cancel.
	done := make(chan struct{})
	var killed sync.WaitGroup
	killed.Add(1)
	go func() {
		defer killed.Done()
		select {
		case <-ctx.Done():
			killGroup(cmd)
		case <-done:
		}
	}()

	// Feed the request. The worker may exit without reading it all; that
	// shows up as a write error that we ignore in favour of the real cause.
	writeErr := make(chan error, 1)
	go func() {
		err := protocol.WriteRequest(stdin, h, payload)
		stdin.Close()
		writeErr <- err
	}()

	rep, out, readErr := protocol.ReadResponse(stdout, h.Limits.MaxOutputBytes)
	if readErr != nil {
		// Stop reading; make sure the worker can't hang on a full pipe.
		killGroup(cmd)
	}
	waitErr := cmd.Wait()
	close(done)
	killed.Wait()
	werr := <-writeErr
	se := stderr.String()

	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fail(se, ctxErr, "timeout after %v", r.Timeout)
		}
		return fail(se, ctxErr, "canceled: %v", ctxErr)
	}
	if errors.Is(werr, protocol.ErrFrameTooBig) {
		// Our own request was too large: a caller error, not a worker one.
		return fail(se, werr, "request: %v", werr)
	}
	if readErr == nil && waitErr != nil {
		return fail(se, waitErr, "worker exited abnormally: %v", waitErr)
	}
	if readErr != nil {
		if waitErr != nil {
			return fail(se, readErr, "%v; worker exit: %v", readErr, waitErr)
		}
		return fail(se, readErr, "bad response: %v", readErr)
	}
	if err := protocol.ValidateReport(rep, out, h.Limits.MaxOutputBytes); err != nil {
		return fail(se, err, "inconsistent report: %v", err)
	}
	return rep, out, nil
}
