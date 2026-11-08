package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

var (
	buildOnce sync.Once
	fakePath  string
	buildErr  error
	buildDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

func fakeWorker(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		// Build once for the package, in a dir that outlives single tests.
		buildDir, buildErr = os.MkdirTemp("", "fakeworker")
		if buildErr != nil {
			return
		}
		name := "fakeworker"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		fakePath = filepath.Join(buildDir, name)
		out, err := exec.Command("go", "build", "-o", fakePath, "./internal/fakeworker").CombinedOutput()
		if err != nil {
			buildErr = errors.New(string(out))
		}
	})
	if buildErr != nil {
		t.Fatalf("building fake worker: %v", buildErr)
	}
	return fakePath
}

func header() protocol.Header {
	l := protocol.DefaultLimits()
	l.MaxInputBytes = 1 << 20
	l.MaxOutputBytes = 1 << 20
	return protocol.NewHeader(protocol.DefaultPolicy(), l)
}

func runner(t *testing.T, mode string, extra ...string) *Runner {
	return &Runner{
		Path:    fakeWorker(t),
		Timeout: 10 * time.Second,
		Env:     append([]string{"FAKEWORKER_MODE=" + mode}, extra...),
	}
}

func TestRunSuccess(t *testing.T) {
	payload := []byte("hello kilnfile")
	rep, out, err := runner(t, "echo").Run(context.Background(), header(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != protocol.ResultUnchanged || !bytes.Equal(out, payload) || rep.Output == nil {
		t.Fatalf("%+v %q", rep, out)
	}

	rep, out, err = runner(t, "reject").Run(context.Background(), header(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != protocol.ResultRejected || rep.Reason != "unknown_type" || rep.Output != nil || len(out) != 0 {
		t.Fatalf("%+v %q", rep, out)
	}
}

func TestRunFailureModes(t *testing.T) {
	small := header()
	small.Limits.MaxOutputBytes = 512
	tests := []struct {
		mode   string
		h      protocol.Header
		detail string
	}{
		{"crash", header(), "exit status 3"},
		{"garbage", header(), ""},
		{"badjson", header(), "invalid report JSON"},
		{"oversize", small, "exceeds limit"},
		{"short", header(), "short read"},
		{"badreport", header(), "inconsistent report"},
		{"unknownresult", header(), "unknown result"},
		{"nonsense-mode", header(), "exit status 2"},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			rep, out, err := runner(t, tc.mode).Run(context.Background(), tc.h, []byte("data"))
			if err == nil {
				t.Fatal("expected error")
			}
			var f *Failure
			if !errors.As(err, &f) {
				t.Fatalf("error is %T", err)
			}
			if rep.Result != protocol.ResultRejected || rep.Reason != protocol.ReasonWorkerFail || rep.Output != nil || len(out) != 0 {
				t.Fatalf("report: %+v", rep)
			}
			if !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("error %q lacks %q", err, tc.detail)
			}
		})
	}
}

func TestCrashCapturesStderr(t *testing.T) {
	_, _, err := runner(t, "crash").Run(context.Background(), header(), nil)
	if err == nil || !strings.Contains(err.Error(), "crashing on purpose") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	r := runner(t, "hang")
	r.Timeout = 300 * time.Millisecond
	start := time.Now()
	rep, _, err := r.Run(context.Background(), header(), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("not wrapping DeadlineExceeded: %v", err)
	}
	if rep.Reason != protocol.ReasonWorkerFail {
		t.Errorf("%+v", rep)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestContextCancel(t *testing.T) {
	r := runner(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	rep, _, err := r.Run(ctx, header(), nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err: %v", err)
	}
	if rep.Reason != protocol.ReasonWorkerFail {
		t.Errorf("%+v", rep)
	}
	// Already-canceled context never starts the worker.
	if _, _, err := r.Run(ctx, header(), nil); err == nil {
		t.Error("canceled context ran")
	}
}

func TestRequestTooLarge(t *testing.T) {
	h := header()
	h.Limits.MaxInputBytes = 3
	_, _, err := runner(t, "echo").Run(context.Background(), h, []byte("four"))
	if err == nil || !strings.Contains(err.Error(), "request") {
		t.Fatalf("err: %v", err)
	}
}

func TestBadWorkerPath(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "nope")} {
		rep, _, err := (&Runner{Path: path}).Run(context.Background(), header(), nil)
		if err == nil || rep.Reason != protocol.ReasonWorkerFail {
			t.Errorf("path %q: %v %+v", path, err, rep)
		}
	}
}

func TestLargePayload(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefgh"), 512<<10/8*3) // 1.5 MiB
	h := header()
	h.Limits.MaxInputBytes = 4 << 20
	h.Limits.MaxOutputBytes = 4 << 20
	_, out, err := runner(t, "echo").Run(context.Background(), h, payload)
	if err != nil || !bytes.Equal(out, payload) {
		t.Fatalf("err=%v len=%d", err, len(out))
	}
}

func TestBoundedBuffer(t *testing.T) {
	b := &boundedBuffer{cap: 10}
	for i := 0; i < 5; i++ {
		if n, err := b.Write([]byte("abcdef")); n != 6 || err != nil {
			t.Fatal(n, err)
		}
	}
	if b.String() != "abcdefabcd" {
		t.Fatalf("%q", b.String())
	}
}
