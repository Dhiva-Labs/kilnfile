package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func smallLimits() Limits {
	l := DefaultLimits()
	l.MaxInputBytes = 100
	l.MaxOutputBytes = 200
	return l
}

func TestRequestRoundTrip(t *testing.T) {
	p := DefaultPolicy()
	p.Types = map[string]TypePolicy{"ooxml": {OnDangerous: "reject"}}
	for _, payload := range [][]byte{nil, []byte("x"), bytes.Repeat([]byte{7}, 100)} {
		h := NewHeader(p, smallLimits())
		var buf bytes.Buffer
		if err := WriteRequest(&buf, h, payload); err != nil {
			t.Fatal(err)
		}
		gh, gp, err := ReadRequest(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gh, h) || !bytes.Equal(gp, payload) {
			t.Fatalf("round trip mismatch: %+v", gh)
		}
	}
}

func TestResponseRoundTrip(t *testing.T) {
	rep := Report{
		KilnfileVersion: "0.1.0", Result: ResultSanitized,
		Input:   &InputInfo{DetectedType: "docm", Bytes: 3, SHA256: "aa"},
		Output:  &OutputInfo{Type: "docx", Bytes: 2, SHA256: "bb"},
		Actions: []Action{{Action: "removed", Part: "a", Reason: "vba_macro"}},
	}
	var buf bytes.Buffer
	if err := WriteResponse(&buf, rep, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, out, err := ReadResponse(&buf, 200)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hi" || got.Result != ResultSanitized || got.Actions[0].Reason != "vba_macro" || got.Output.Type != "docx" {
		t.Fatalf("mismatch: %+v %q", got, out)
	}
}

func frame4(n int, body []byte) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(n))
	return append(b, body...)
}

func frame8(n uint64, body []byte) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return append(b, body...)
}

func TestWriteLimits(t *testing.T) {
	h := NewHeader(DefaultPolicy(), smallLimits())
	if err := WriteRequest(io.Discard, h, make([]byte, 101)); !errors.Is(err, ErrFrameTooBig) {
		t.Errorf("oversized payload: %v", err)
	}
	if err := WriteRequest(io.Discard, h, make([]byte, 100)); err != nil {
		t.Errorf("payload at limit: %v", err)
	}
	h.Policy.Types = map[string]TypePolicy{strings.Repeat("k", MaxJSONFrame): {}}
	if err := WriteRequest(io.Discard, h, nil); !errors.Is(err, ErrFrameTooBig) {
		t.Errorf("oversized header: %v", err)
	}
	rep := Report{Result: ResultRejected, Reason: strings.Repeat("r", MaxJSONFrame)}
	if err := WriteResponse(io.Discard, rep, nil); !errors.Is(err, ErrFrameTooBig) {
		t.Errorf("oversized report: %v", err)
	}
	if err := WriteResponse(io.Discard, Report{Result: ResultRejected, Reason: "x"}, []byte("o")); err == nil {
		t.Error("rejected with output accepted")
	}
}

func TestReadRequestErrors(t *testing.T) {
	good := []byte(`{"protocol":1,"limits":{"max_input_bytes":4}}`)
	tests := []struct {
		name  string
		data  []byte
		want  error // nil means any non-nil error
		short bool
	}{
		{"empty", nil, ErrShortRead, true},
		{"short header len", []byte{1, 2}, ErrShortRead, true},
		{"header over cap", frame4(MaxJSONFrame+1, nil), ErrFrameTooBig, false},
		{"short header body", frame4(40, []byte("{}")), ErrShortRead, true},
		{"bad json", frame4(3, []byte("{x}")), nil, false},
		{"no payload len", frame4(len(good), good), ErrShortRead, true},
		{"payload over limit", append(frame4(len(good), good), frame8(5, nil)...), ErrFrameTooBig, false},
		{"payload huge", append(frame4(len(good), good), frame8(1<<63+1, nil)...), ErrFrameTooBig, false},
		{"short payload", append(frame4(len(good), good), frame8(4, []byte("ab"))...), ErrShortRead, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ReadRequest(bytes.NewReader(tc.data))
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	// Exactly at the limit works.
	data := append(frame4(len(good), good), frame8(4, []byte("abcd"))...)
	if _, p, err := ReadRequest(bytes.NewReader(data)); err != nil || string(p) != "abcd" {
		t.Fatalf("at limit: %v %q", err, p)
	}
}

func TestReadResponseErrors(t *testing.T) {
	rep := []byte(`{"result":"sanitized"}`)
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrShortRead},
		{"report over cap", frame4(MaxJSONFrame+1, nil), ErrFrameTooBig},
		{"short report", frame4(50, rep), ErrShortRead},
		{"bad json", append(frame4(3, []byte("{x}")), frame8(0, nil)...), nil},
		{"no output len", frame4(len(rep), rep), ErrShortRead},
		{"output over limit", append(frame4(len(rep), rep), frame8(11, nil)...), ErrFrameTooBig},
		{"short output", append(frame4(len(rep), rep), frame8(5, []byte("ab"))...), ErrShortRead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ReadResponse(bytes.NewReader(tc.data), 10)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateReport(t *testing.T) {
	out := &OutputInfo{Type: "x"}
	tests := []struct {
		name   string
		rep    Report
		output []byte
		ok     bool
	}{
		{"sanitized", Report{Result: ResultSanitized, Output: out}, []byte("a"), true},
		{"unchanged", Report{Result: ResultUnchanged, Output: out}, nil, true},
		{"rejected", Report{Result: ResultRejected, Reason: "unparseable"}, nil, true},
		{"unknown result", Report{Result: "banana"}, nil, false},
		{"empty result", Report{}, nil, false},
		{"sanitized null output", Report{Result: ResultSanitized}, nil, false},
		{"rejected with output obj", Report{Result: ResultRejected, Output: out}, nil, false},
		{"rejected with output bytes", Report{Result: ResultRejected}, []byte("a"), false},
		{"output too big", Report{Result: ResultSanitized, Output: out}, make([]byte, 11), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReport(tc.rep, tc.output, 10)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v ok=%v", err, tc.ok)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	p, l := DefaultPolicy(), DefaultLimits()
	if p.OnDangerous != "remove" || p.OnUnknownType != "reject" || p.AnimatedImages != "first_frame" || p.AllowHTTPLinks || p.KeepICCProfile {
		t.Errorf("policy defaults: %+v", p)
	}
	if l.MaxInputBytes != 104857600 || l.MaxOutputBytes != 209715200 || l.MaxDecodedPixels != 100000000 ||
		l.MaxDecodeMemoryBytes != 536870912 || l.MaxNestingDepth != 3 || l.MaxArchiveEntries != 10000 ||
		l.MaxTotalUncompressedBytes != 1073741824 || l.MaxCompressionRatio != 100 || l.MaxStructuredDepth != 256 {
		t.Errorf("limit defaults: %+v", l)
	}
}

func TestHeaderJSONNames(t *testing.T) {
	var buf bytes.Buffer
	p := DefaultPolicy()
	p.Types = map[string]TypePolicy{"ooxml": {OnDangerous: "reject"}}
	if err := WriteRequest(&buf, NewHeader(p, DefaultLimits()), nil); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, k := range []string{`"protocol":1`, `"on_dangerous":"remove"`, `"on_unknown_type":"reject"`, `"allow_http_links":false`,
		`"keep_icc_profile":false`, `"animated_images":"first_frame"`, `"types":{"ooxml":{"on_dangerous":"reject"}}`,
		`"max_input_bytes":104857600`, `"max_decoded_pixels"`, `"max_decode_memory_bytes"`, `"max_nesting_depth"`,
		`"max_archive_entries"`, `"max_total_uncompressed_bytes"`, `"max_compression_ratio":100`, `"max_structured_depth":256`} {
		if !strings.Contains(s, k) {
			t.Errorf("header missing %s", k)
		}
	}
}
