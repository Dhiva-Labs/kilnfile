package worker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "w")
	os.WriteFile(good, []byte("x"), 0o755)
	t.Setenv(WorkerEnv, "")

	if p, err := Locate(good); err != nil || p != good {
		t.Errorf("explicit: %v %q", p, err)
	}
	if _, err := Locate(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing explicit accepted")
	}
	if _, err := Locate(dir); err == nil {
		t.Error("directory accepted")
	}
	t.Setenv(WorkerEnv, good)
	if p, err := Locate(""); err != nil || p != good {
		t.Errorf("env: %v %q", p, err)
	}
	// Explicit beats env.
	other := filepath.Join(dir, "o")
	os.WriteFile(other, []byte("x"), 0o755)
	if p, _ := Locate(other); p != other {
		t.Errorf("explicit should win, got %q", p)
	}
	if runtime.GOOS != "windows" {
		noexec := filepath.Join(dir, "n")
		os.WriteFile(noexec, []byte("x"), 0o644)
		if _, err := Locate(noexec); err == nil {
			t.Error("non-executable accepted")
		}
	}
	t.Setenv(WorkerEnv, "")
	t.Setenv("PATH", dir)
	if _, err := Locate(""); err == nil {
		t.Error("expected not found")
	}
	os.WriteFile(filepath.Join(dir, BinaryName()), []byte("x"), 0o755)
	if p, err := Locate(""); err != nil || filepath.Base(p) != BinaryName() {
		t.Errorf("PATH: %v %q", p, err)
	}
}
