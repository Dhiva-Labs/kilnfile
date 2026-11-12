//go:build !windows

package worker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestTimeoutKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	r := runner(t, "hangchild", "FAKEWORKER_PIDFILE="+pidFile)
	r.Timeout = time.Second
	if _, _, err := r.Run(context.Background(), header(), nil); err == nil {
		t.Fatal("expected timeout")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("child never started: %v", err)
	}
	pid, _ := strconv.Atoi(string(data))
	deadline := time.Now().Add(3 * time.Second)
	for {
		// Signal 0 probes existence; a zombie child of init may linger briefly.
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d survived the group kill", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
