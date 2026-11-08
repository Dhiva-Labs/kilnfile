package worker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// WorkerEnv is the environment variable that names the worker executable.
const WorkerEnv = "KILNFILE_WORKER"

// BinaryName is the worker's file name.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "kilnfile-worker.exe"
	}
	return "kilnfile-worker"
}

// Locate finds the worker executable: the explicit path if given, else
// $KILNFILE_WORKER, else kilnfile-worker next to the running executable, else
// kilnfile-worker on PATH.
func Locate(explicit string) (string, error) {
	if explicit != "" {
		return checkExecutable(explicit)
	}
	if env := os.Getenv(WorkerEnv); env != "" {
		return checkExecutable(env)
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			cand := filepath.Join(filepath.Dir(exe), BinaryName())
			if p, err := checkExecutable(cand); err == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(BinaryName()); err == nil {
		return p, nil
	}
	return "", errors.New("kilnfile-worker not found: use --worker, set " + WorkerEnv +
		", or put it next to kilnfile or on PATH")
}

func checkExecutable(p string) (string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("worker %s: %w", p, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("worker %s is a directory", p)
	}
	if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		return "", fmt.Errorf("worker %s is not executable", p)
	}
	return p, nil
}
