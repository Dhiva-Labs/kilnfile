package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dhiva-Labs/kilnfile/internal/protocol"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(t *testing.T, c Config)
	}{
		{"empty gives defaults", "", "", func(t *testing.T, c Config) {
			if c.Policy.OnDangerous != "remove" || c.Limits != protocol.DefaultLimits() {
				t.Errorf("%+v", c)
			}
		}},
		{"comment only", "# nothing\n", "", nil},
		{"partial merge", "on_dangerous: reject\nallow_http_links: true\n", "", func(t *testing.T, c Config) {
			if c.OnDangerous != "reject" || !c.AllowHTTPLinks || c.OnUnknownType != "reject" || c.AnimatedImages != "first_frame" {
				t.Errorf("%+v", c)
			}
		}},
		{"types and limits", "types:\n  ooxml:\n    on_dangerous: reject\n  image:\n    on_dangerous: allow\nlimits:\n  max_input_bytes: 1000\n", "", func(t *testing.T, c Config) {
			if c.Types["ooxml"].OnDangerous != "reject" || c.Types["image"].OnDangerous != "allow" {
				t.Errorf("%+v", c.Types)
			}
			if c.Limits.MaxInputBytes != 1000 || c.Limits.MaxOutputBytes != protocol.DefaultLimits().MaxOutputBytes {
				t.Errorf("%+v", c.Limits)
			}
		}},
		{"all enums", "on_dangerous: allow\non_unknown_type: allow\nanimated_images: keep_animation\nkeep_icc_profile: true\n", "", func(t *testing.T, c Config) {
			if c.AnimatedImages != "keep_animation" || !c.KeepICCProfile || c.OnUnknownType != "allow" {
				t.Errorf("%+v", c)
			}
		}},
		{"bad on_dangerous", "on_dangerous: delete\n", `invalid on_dangerous "delete"`, nil},
		{"bad on_unknown_type", "on_unknown_type: remove\n", "invalid on_unknown_type", nil},
		{"bad animated", "animated_images: all\n", "invalid animated_images", nil},
		{"bad type key", "types:\n  pdf:\n    on_dangerous: reject\n", `invalid types key "pdf"`, nil},
		{"bad type value", "types:\n  ooxml:\n    on_dangerous: nope\n", "types.ooxml.on_dangerous", nil},
		{"empty type override", "types:\n  ooxml: {}\n", "types.ooxml.on_dangerous", nil},
		{"unknown field", "on_danger: remove\n", "on_danger", nil},
		{"zero limit", "limits:\n  max_compression_ratio: 0\n", "max_compression_ratio", nil},
		{"negative limit", "limits:\n  max_input_bytes: -1\n", "", nil},
		{"wrong type", "allow_http_links: maybe\n", "", nil},
		{"bad yaml", "on_dangerous: [\n", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse(strings.NewReader(tc.yaml))
			wantErr := tc.wantErr != "" || strings.HasPrefix(tc.name, "bad") || tc.name == "negative limit" || tc.name == "wrong type"
			if wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, c)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	if err := os.WriteFile(path, []byte("on_dangerous: reject\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(path)
	if err != nil || c.OnDangerous != "reject" {
		t.Fatalf("%v %+v", err, c)
	}
	h := c.Header()
	if h.Protocol != 1 || h.Policy.OnDangerous != "reject" {
		t.Errorf("%+v", h)
	}
	if _, err := LoadFile(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("missing file accepted")
	}
	os.WriteFile(path, []byte("on_dangerous: bogus\n"), 0o600)
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file: %v", err)
	}
}
