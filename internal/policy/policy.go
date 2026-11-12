// Package policy loads and validates kilnfile policy files.
package policy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

// Config is the content of a policy file: the policy fields at the top level,
// plus an optional "limits" block. Anything not set keeps its default.
type Config struct {
	protocol.Policy `yaml:",inline"`
	Limits          protocol.Limits `yaml:"limits"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{Policy: protocol.DefaultPolicy(), Limits: protocol.DefaultLimits()}
}

// Header builds the complete request header for the configuration.
func (c Config) Header() protocol.Header {
	return protocol.NewHeader(c.Policy, c.Limits)
}

// LoadFile reads a YAML policy file merged over the defaults.
func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	c, err := Parse(f)
	if err != nil {
		return Config{}, fmt.Errorf("policy %s: %w", path, err)
	}
	return c, nil
}

// Parse reads YAML from r merged over the defaults, and validates it. Unknown
// fields are an error, to catch typos in a security policy.
func Parse(r io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20+1))
	if err != nil {
		return Config{}, err
	}
	if len(data) > 1<<20 {
		return Config{}, errors.New("policy file larger than 1 MiB")
	}
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

var (
	dangerousValues = []string{"remove", "reject", "allow"}
	unknownValues   = []string{"reject", "allow"}
	animatedValues  = []string{"first_frame", "keep_animation"}
	typeKeys        = []string{"image", "ooxml", "archive", "structured"}
)

func oneOf(field, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q: must be one of %v", field, v, allowed)
}

// Validate checks enum values, type keys and limits.
func (c Config) Validate() error {
	if err := oneOf("on_dangerous", c.OnDangerous, dangerousValues); err != nil {
		return err
	}
	if err := oneOf("on_unknown_type", c.OnUnknownType, unknownValues); err != nil {
		return err
	}
	if err := oneOf("animated_images", c.AnimatedImages, animatedValues); err != nil {
		return err
	}
	keys := make([]string, 0, len(c.Types))
	for k := range c.Types {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := oneOf("types key", k, typeKeys); err != nil {
			return err
		}
		if err := oneOf("types."+k+".on_dangerous", c.Types[k].OnDangerous, dangerousValues); err != nil {
			return err
		}
	}
	l := c.Limits
	for name, v := range map[string]uint64{
		"max_input_bytes":              l.MaxInputBytes,
		"max_output_bytes":             l.MaxOutputBytes,
		"max_decoded_pixels":           l.MaxDecodedPixels,
		"max_decode_memory_bytes":      l.MaxDecodeMemoryBytes,
		"max_nesting_depth":            l.MaxNestingDepth,
		"max_archive_entries":          l.MaxArchiveEntries,
		"max_total_uncompressed_bytes": l.MaxTotalUncompressedBytes,
		"max_compression_ratio":        l.MaxCompressionRatio,
		"max_structured_depth":         l.MaxStructuredDepth,
	} {
		if v == 0 {
			return fmt.Errorf("limits.%s must be greater than 0", name)
		}
	}
	return nil
}
