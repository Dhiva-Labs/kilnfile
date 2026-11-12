package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// Framing errors.
var (
	ErrShortRead   = errors.New("protocol: short read")
	ErrFrameTooBig = errors.New("protocol: frame exceeds limit")
)

func readFull(r io.Reader, buf []byte, what string) error {
	if _, err := io.ReadFull(r, buf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("%w: %s", ErrShortRead, what)
		}
		return fmt.Errorf("protocol: reading %s: %w", what, err)
	}
	return nil
}

// readBlob reads a length-prefixed blob: a length of width 4 or 8 bytes, then
// the data. The length is checked against max before anything is allocated.
func readBlob(r io.Reader, width int, max uint64, what string) ([]byte, error) {
	var lenBuf [8]byte
	if err := readFull(r, lenBuf[:width], what+" length"); err != nil {
		return nil, err
	}
	var n uint64
	if width == 4 {
		n = uint64(binary.LittleEndian.Uint32(lenBuf[:4]))
	} else {
		n = binary.LittleEndian.Uint64(lenBuf[:8])
	}
	if n > max || n > math.MaxInt64 {
		return nil, fmt.Errorf("%w: %s is %d bytes, limit %d", ErrFrameTooBig, what, n, max)
	}
	// Don't trust the declared length for allocation: grow as data arrives.
	var buf bytes.Buffer
	if n <= 64<<10 {
		buf.Grow(int(n))
	}
	if _, err := io.CopyN(&buf, r, int64(n)); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("%w: %s (got %d of %d bytes)", ErrShortRead, what, buf.Len(), n)
		}
		return nil, fmt.Errorf("protocol: reading %s: %w", what, err)
	}
	return buf.Bytes(), nil
}

func writeBlob(w io.Writer, width int, data []byte) error {
	var lenBuf [8]byte
	if width == 4 {
		binary.LittleEndian.PutUint32(lenBuf[:4], uint32(len(data)))
	} else {
		binary.LittleEndian.PutUint64(lenBuf[:8], uint64(len(data)))
	}
	if _, err := w.Write(lenBuf[:width]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// WriteRequest writes a request frame (CLI side). It enforces the header cap
// and the max_input_bytes limit.
func WriteRequest(w io.Writer, h Header, payload []byte) error {
	hb, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if len(hb) > MaxJSONFrame {
		return fmt.Errorf("%w: header is %d bytes, limit %d", ErrFrameTooBig, len(hb), MaxJSONFrame)
	}
	if uint64(len(payload)) > h.Limits.MaxInputBytes {
		return fmt.Errorf("%w: payload is %d bytes, limit %d", ErrFrameTooBig, len(payload), h.Limits.MaxInputBytes)
	}
	if err := writeBlob(w, 4, hb); err != nil {
		return err
	}
	return writeBlob(w, 8, payload)
}

// ReadRequest reads a request frame (worker side).
func ReadRequest(r io.Reader) (Header, []byte, error) {
	var h Header
	hb, err := readBlob(r, 4, MaxJSONFrame, "header")
	if err != nil {
		return h, nil, err
	}
	if err := json.Unmarshal(hb, &h); err != nil {
		return h, nil, fmt.Errorf("protocol: invalid header JSON: %w", err)
	}
	payload, err := readBlob(r, 8, h.Limits.MaxInputBytes, "payload")
	if err != nil {
		return h, nil, err
	}
	return h, payload, nil
}

// WriteResponse writes a response frame (worker side). It enforces the report
// cap and that output is empty for rejected results.
func WriteResponse(w io.Writer, rep Report, output []byte) error {
	rb, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	if len(rb) > MaxJSONFrame {
		return fmt.Errorf("%w: report is %d bytes, limit %d", ErrFrameTooBig, len(rb), MaxJSONFrame)
	}
	if rep.Result == ResultRejected && len(output) != 0 {
		return errors.New("protocol: rejected result must have empty output")
	}
	if err := writeBlob(w, 4, rb); err != nil {
		return err
	}
	return writeBlob(w, 8, output)
}

// ReadResponse reads a response frame (CLI side). The report is decoded but
// not semantically validated; see ValidateReport. maxOutput bounds output_len.
func ReadResponse(r io.Reader, maxOutput uint64) (Report, []byte, error) {
	var rep Report
	rb, err := readBlob(r, 4, MaxJSONFrame, "report")
	if err != nil {
		return rep, nil, err
	}
	if err := json.Unmarshal(rb, &rep); err != nil {
		return rep, nil, fmt.Errorf("protocol: invalid report JSON: %w", err)
	}
	out, err := readBlob(r, 8, maxOutput, "output")
	if err != nil {
		return rep, nil, err
	}
	return rep, out, nil
}

// ValidateReport checks a decoded report against the received output.
func ValidateReport(rep Report, output []byte, maxOutput uint64) error {
	switch rep.Result {
	case ResultSanitized, ResultUnchanged:
		if rep.Output == nil {
			return errors.New("protocol: non-rejected report has null output")
		}
	case ResultRejected:
		if rep.Output != nil {
			return errors.New("protocol: rejected report must have null output")
		}
		if len(output) != 0 {
			return errors.New("protocol: rejected report must have output_len 0")
		}
	default:
		return fmt.Errorf("protocol: unknown result %q", rep.Result)
	}
	if uint64(len(output)) > maxOutput {
		return fmt.Errorf("%w: output is %d bytes, limit %d", ErrFrameTooBig, len(output), maxOutput)
	}
	return nil
}
