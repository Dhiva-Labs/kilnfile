// Package protocol defines the wire types and framing for the kilnfile
// worker protocol (see docs/PROTOCOL.md).
package protocol

// Version is the protocol version carried in the request header.
const Version = 1

// MaxJSONFrame is the largest allowed header or report, in bytes.
const MaxJSONFrame = 1 << 20

// Result values.
const (
	ResultSanitized  = "sanitized"
	ResultUnchanged  = "unchanged_semantics"
	ResultRejected   = "rejected"
	ReasonWorkerFail = "worker_failure"
)

// Header is the request header.
type Header struct {
	Protocol int    `json:"protocol"`
	Policy   Policy `json:"policy"`
	Limits   Limits `json:"limits"`
}

// TypePolicy is a per-type-family override.
type TypePolicy struct {
	OnDangerous string `json:"on_dangerous,omitempty" yaml:"on_dangerous"`
}

// Policy controls what the worker does with dangerous content.
type Policy struct {
	OnDangerous    string                `json:"on_dangerous" yaml:"on_dangerous"`
	OnUnknownType  string                `json:"on_unknown_type" yaml:"on_unknown_type"`
	AllowHTTPLinks bool                  `json:"allow_http_links" yaml:"allow_http_links"`
	KeepICCProfile bool                  `json:"keep_icc_profile" yaml:"keep_icc_profile"`
	AnimatedImages string                `json:"animated_images" yaml:"animated_images"`
	Types          map[string]TypePolicy `json:"types,omitempty" yaml:"types"`
}

// Limits are the resource limits the worker enforces.
type Limits struct {
	MaxInputBytes             uint64 `json:"max_input_bytes" yaml:"max_input_bytes"`
	MaxOutputBytes            uint64 `json:"max_output_bytes" yaml:"max_output_bytes"`
	MaxDecodedPixels          uint64 `json:"max_decoded_pixels" yaml:"max_decoded_pixels"`
	MaxDecodeMemoryBytes      uint64 `json:"max_decode_memory_bytes" yaml:"max_decode_memory_bytes"`
	MaxNestingDepth           uint64 `json:"max_nesting_depth" yaml:"max_nesting_depth"`
	MaxArchiveEntries         uint64 `json:"max_archive_entries" yaml:"max_archive_entries"`
	MaxTotalUncompressedBytes uint64 `json:"max_total_uncompressed_bytes" yaml:"max_total_uncompressed_bytes"`
	MaxCompressionRatio       uint64 `json:"max_compression_ratio" yaml:"max_compression_ratio"`
	MaxStructuredDepth        uint64 `json:"max_structured_depth" yaml:"max_structured_depth"`
}

// InputInfo describes the input file.
type InputInfo struct {
	DetectedType string `json:"detected_type"`
	Bytes        uint64 `json:"bytes"`
	SHA256       string `json:"sha256"`
}

// OutputInfo describes the output file.
type OutputInfo struct {
	Type   string `json:"type"`
	Bytes  uint64 `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Action is one removal, rewrite or rejection.
type Action struct {
	Action string `json:"action"`
	Part   string `json:"part"`
	Reason string `json:"reason"`
}

// Report is the worker's account of what it did.
type Report struct {
	KilnfileVersion string      `json:"kilnfile_version"`
	Result          string      `json:"result"`
	Reason          string      `json:"reason"`
	Input           *InputInfo  `json:"input"`
	Output          *OutputInfo `json:"output"`
	Actions         []Action    `json:"actions"`
	LimitsHit       []string    `json:"limits_hit"`
}

// DefaultPolicy returns the documented default policy.
func DefaultPolicy() Policy {
	return Policy{
		OnDangerous:    "remove",
		OnUnknownType:  "reject",
		AllowHTTPLinks: false,
		KeepICCProfile: false,
		AnimatedImages: "first_frame",
	}
}

// DefaultLimits returns the documented default limits.
func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:             104857600,
		MaxOutputBytes:            209715200,
		MaxDecodedPixels:          100000000,
		MaxDecodeMemoryBytes:      536870912,
		MaxNestingDepth:           3,
		MaxArchiveEntries:         10000,
		MaxTotalUncompressedBytes: 1073741824,
		MaxCompressionRatio:       100,
		MaxStructuredDepth:        256,
	}
}

// NewHeader builds a complete header from a policy and limits.
func NewHeader(p Policy, l Limits) Header {
	return Header{Protocol: Version, Policy: p, Limits: l}
}

// Rejected builds the Go-side report for a failed worker.
func Rejected(reason string) Report {
	return Report{
		KilnfileVersion: "",
		Result:          ResultRejected,
		Reason:          reason,
		Actions:         []Action{},
		LimitsHit:       []string{},
	}
}
