package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

var kilnfileBin, fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kilnfile-e2e")
	if err != nil {
		panic(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	kilnfileBin = filepath.Join(dir, "kilnfile"+ext)
	fakeBin = filepath.Join(dir, "fakeworker"+ext)
	for out, pkg := range map[string]string{kilnfileBin: ".", fakeBin: "../../internal/worker/internal/fakeworker"} {
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			os.RemoveAll(dir)
			panic("build " + pkg + ": " + string(b))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code   int
	stderr string
}

func kilnfile(t *testing.T, mode string, args ...string) result {
	t.Helper()
	cmd := exec.Command(kilnfileBin, args...)
	cmd.Env = append(os.Environ(), "FAKEWORKER_MODE="+mode)
	var se bytes.Buffer
	cmd.Stderr = &se
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, se.String()}
}

func setup(t *testing.T, content string) (dir, in, out, rep string) {
	dir = t.TempDir()
	in = filepath.Join(dir, "in.bin")
	if err := os.WriteFile(in, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, in, filepath.Join(dir, "out.bin"), filepath.Join(dir, "report.json")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func readReport(t *testing.T, p string) protocol.Report {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var r protocol.Report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSanitizeEcho(t *testing.T) {
	dir, in, out, rep := setup(t, "hello world")
	r := kilnfile(t, "echo", "sanitize", in, "-o", out, "--worker", fakeBin, "--report", rep)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if b, _ := os.ReadFile(out); string(b) != "hello world" {
		t.Errorf("output %q", b)
	}
	rp := readReport(t, rep)
	if rp.Result != protocol.ResultUnchanged || rp.Output == nil || rp.Input.Bytes != 11 {
		t.Errorf("%+v", rp)
	}
	if !strings.Contains(r.stderr, "unchanged_semantics") {
		t.Errorf("summary: %q", r.stderr)
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestSanitizeReject(t *testing.T) {
	_, in, out, rep := setup(t, "x")
	r := kilnfile(t, "reject", "sanitize", in, "-o", out, "--worker", fakeBin, "--report", rep)
	if r.code != 2 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if exists(out) {
		t.Error("output written on reject")
	}
	rp := readReport(t, rep)
	if rp.Result != protocol.ResultRejected || rp.Reason != "unknown_type" || rp.Output != nil {
		t.Errorf("%+v", rp)
	}
	if !strings.Contains(r.stderr, "rejected") {
		t.Errorf("summary: %q", r.stderr)
	}
}

func TestSanitizeReject_ForceKeepsExistingOutput(t *testing.T) {
	_, in, out, _ := setup(t, "x")
	os.WriteFile(out, []byte("precious"), 0o600)
	if r := kilnfile(t, "reject", "sanitize", in, "-o", out, "--worker", fakeBin, "--force"); r.code != 2 {
		t.Fatalf("exit %d", r.code)
	}
	if b, _ := os.ReadFile(out); string(b) != "precious" {
		t.Errorf("existing output modified: %q", b)
	}
}

func TestWorkerFailure(t *testing.T) {
	for _, mode := range []string{"crash", "garbage", "badjson", "short", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			_, in, out, rep := setup(t, "x")
			r := kilnfile(t, mode, "sanitize", in, "-o", out, "--worker", fakeBin, "--report", rep)
			if r.code != 2 || exists(out) {
				t.Fatalf("exit %d, output exists=%v", r.code, exists(out))
			}
			rp := readReport(t, rep)
			if rp.Result != protocol.ResultRejected || rp.Reason != "worker_failure" {
				t.Errorf("%+v", rp)
			}
			if !strings.Contains(r.stderr, "worker failure") {
				t.Errorf("stderr lacks detail: %q", r.stderr)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	_, in, out, _ := setup(t, "x")
	r := kilnfile(t, "hang", "sanitize", in, "-o", out, "--worker", fakeBin, "--timeout", "300ms")
	if r.code != 2 || exists(out) || !strings.Contains(r.stderr, "timeout") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}

func TestNoOverwriteWithoutForce(t *testing.T) {
	_, in, out, _ := setup(t, "new")
	os.WriteFile(out, []byte("old"), 0o600)
	r := kilnfile(t, "echo", "sanitize", in, "-o", out, "--worker", fakeBin)
	if r.code != 1 || !strings.Contains(r.stderr, "--force") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if b, _ := os.ReadFile(out); string(b) != "old" {
		t.Errorf("overwritten: %q", b)
	}
	if r := kilnfile(t, "echo", "sanitize", in, "-o", out, "--worker", fakeBin, "--force"); r.code != 0 {
		t.Fatalf("force: exit %d: %s", r.code, r.stderr)
	}
	if b, _ := os.ReadFile(out); string(b) != "new" {
		t.Errorf("not overwritten: %q", b)
	}
}

func TestOutputSameAsInput(t *testing.T) {
	_, in, _, _ := setup(t, "data")
	if r := kilnfile(t, "echo", "sanitize", in, "-o", in, "--worker", fakeBin, "--force"); r.code != 1 {
		t.Fatalf("exit %d", r.code)
	}
	if b, _ := os.ReadFile(in); string(b) != "data" {
		t.Error("input clobbered")
	}
}

func TestPolicyAndLimits(t *testing.T) {
	dir, in, out, rep := setup(t, "0123456789")
	pol := filepath.Join(dir, "p.yaml")
	os.WriteFile(pol, []byte("limits:\n  max_input_bytes: 5\n"), 0o600)
	r := kilnfile(t, "echo", "sanitize", in, "-o", out, "--worker", fakeBin, "--policy", pol, "--report", rep)
	if r.code != 2 || exists(out) {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	rp := readReport(t, rep)
	if rp.Reason != "limit_exceeded" || len(rp.LimitsHit) != 1 || rp.LimitsHit[0] != "max_input_bytes" {
		t.Errorf("%+v", rp)
	}

	os.WriteFile(pol, []byte("on_dangerous: explode\n"), 0o600)
	r = kilnfile(t, "echo", "sanitize", in, "-o", out, "--worker", fakeBin, "--policy", pol)
	if r.code != 1 || !strings.Contains(r.stderr, "on_dangerous") {
		t.Fatalf("bad policy: exit %d: %s", r.code, r.stderr)
	}
}

func TestWorkerFromEnv(t *testing.T) {
	_, in, out, _ := setup(t, "abc")
	cmd := exec.Command(kilnfileBin, "sanitize", in, "-o", out)
	cmd.Env = append(os.Environ(), "FAKEWORKER_MODE=echo", "KILNFILE_WORKER="+fakeBin)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if b, _ := os.ReadFile(out); string(b) != "abc" {
		t.Errorf("%q", b)
	}
}

func TestUsageErrors(t *testing.T) {
	_, in, out, _ := setup(t, "x")
	tests := []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"unknown command", []string{"frobnicate"}},
		{"no input", []string{"sanitize", "-o", out}},
		{"no output", []string{"sanitize", in}},
		{"two inputs", []string{"sanitize", in, in, "-o", out}},
		{"missing input file", []string{"sanitize", in + ".nope", "-o", out, "--worker", fakeBin}},
		{"missing worker", []string{"sanitize", in, "-o", out, "--worker", in + ".nope"}},
		{"bad flag", []string{"sanitize", in, "-o", out, "--bogus"}},
		{"bad timeout", []string{"sanitize", in, "-o", out, "--timeout", "0s"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if r := kilnfile(t, "echo", tc.args...); r.code != 1 || exists(out) {
				t.Fatalf("exit %d, out=%v: %s", r.code, exists(out), r.stderr)
			}
		})
	}
}

func TestFlagsAfterInput(t *testing.T) {
	_, in, out, _ := setup(t, "x")
	if r := kilnfile(t, "echo", "sanitize", "-o", out, in, "--worker", fakeBin); r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}

func TestVersion(t *testing.T) {
	cmd := exec.Command(kilnfileBin, "version")
	b, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(b), "kilnfile "+Version) {
		t.Fatalf("%v %q", err, b)
	}
}
