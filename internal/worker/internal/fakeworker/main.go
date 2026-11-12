// Command fakeworker stands in for kilnfile-worker in tests. Its behaviour is
// chosen by the FAKEWORKER_MODE environment variable.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func main() {
	mode := os.Getenv("FAKEWORKER_MODE")
	if mode == "hang" {
		time.Sleep(time.Hour)
		return
	}
	if mode == "hangchild" {
		// Spawn a hanging child, record its pid, then hang too. The runner
		// must kill the whole process group, child included.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "FAKEWORKER_MODE=hang")
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		if f := os.Getenv("FAKEWORKER_PIDFILE"); f != "" {
			os.WriteFile(f, []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		}
		time.Sleep(time.Hour)
		return
	}
	h, payload, err := protocol.ReadRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeworker: read request:", err)
		os.Exit(1)
	}
	out := os.Stdout
	switch mode {
	case "echo", "":
		rep := protocol.Report{
			KilnfileVersion: "fake",
			Result:          protocol.ResultUnchanged,
			Input:           &protocol.InputInfo{DetectedType: "fake", Bytes: uint64(len(payload)), SHA256: sum(payload)},
			Output:          &protocol.OutputInfo{Type: "fake", Bytes: uint64(len(payload)), SHA256: sum(payload)},
			Actions:         []protocol.Action{},
			LimitsHit:       []string{},
		}
		if err := protocol.WriteResponse(out, rep, payload); err != nil {
			os.Exit(1)
		}
	case "reject":
		rep := protocol.Report{
			KilnfileVersion: "fake",
			Result:          protocol.ResultRejected,
			Reason:          "unknown_type",
			Input:           &protocol.InputInfo{DetectedType: "unknown", Bytes: uint64(len(payload)), SHA256: sum(payload)},
			Actions:         []protocol.Action{},
			LimitsHit:       []string{},
		}
		if err := protocol.WriteResponse(out, rep, nil); err != nil {
			os.Exit(1)
		}
	case "crash":
		fmt.Fprintln(os.Stderr, "fakeworker: crashing on purpose")
		os.Exit(3)
	case "garbage":
		out.Write([]byte("this is not a frame at all, and is long enough to look like one......"))
	case "badjson":
		bad := []byte("{not json")
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(bad)))
		out.Write(l[:])
		out.Write(bad)
		out.Write(make([]byte, 8))
	case "oversize":
		rb := []byte(`{"result":"sanitized","output":{"type":"fake","bytes":0,"sha256":""}}`)
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(rb)))
		out.Write(l[:])
		out.Write(rb)
		var o [8]byte
		binary.LittleEndian.PutUint64(o[:], h.Limits.MaxOutputBytes+1)
		out.Write(o[:])
		out.Write(make([]byte, 1024))
	case "short":
		rb := []byte(`{"result":"sanitized","output":{"type":"fake","bytes":10,"sha256":""}}`)
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(rb)))
		out.Write(l[:])
		out.Write(rb)
		var o [8]byte
		binary.LittleEndian.PutUint64(o[:], 10)
		out.Write(o[:])
		out.Write([]byte("abc")) // claims 10, sends 3
		os.Exit(0)
	case "badreport":
		// Valid framing, but a rejected report that carries output.
		rb := []byte(`{"result":"rejected","reason":"x","output":{"type":"fake","bytes":1,"sha256":""}}`)
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(rb)))
		out.Write(l[:])
		out.Write(rb)
		out.Write(make([]byte, 8))
	case "unknownresult":
		rb := []byte(`{"result":"banana"}`)
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(rb)))
		out.Write(l[:])
		out.Write(rb)
		out.Write(make([]byte, 8))
	default:
		fmt.Fprintln(os.Stderr, "fakeworker: unknown mode", mode)
		os.Exit(2)
	}
}
