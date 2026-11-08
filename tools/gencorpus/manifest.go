package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Result and reason values follow docs/PROTOCOL.md.
const (
	resSanitized = "sanitized"
	resUnchanged = "unchanged_semantics"
	resRejected  = "rejected"

	rsnUnknownType   = "unknown_type"
	rsnUnparseable   = "unparseable"
	rsnLimitExceeded = "limit_exceeded"
)

// Action reasons used in must_remove. image_reencoded and vba_macro and
// external_template come from the design doc; the rest are named here in the
// same style and are the corpus's proposal for the engine to adopt.
const (
	actVBA         = "vba_macro"
	actExtTemplate = "external_template"
	actActiveX     = "activex"
	actOLE         = "ole_embedding"
	actDDE         = "dde_field"
	actCustomUI    = "custom_ui"
	actDTD         = "dtd"
	actPI          = "processing_instruction"
	actMetadata    = "metadata_removed"
	actTrailing    = "trailing_data"
	actUnsafePath  = "unsafe_path"
	actSymlink     = "symlink"
)

type Expected struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
	// MustRemove lists action reasons that must appear in the report.
	MustRemove []string `json:"must_remove"`
	// MustNotContain lists byte strings or part names absent from the
	// output. A plain string is its UTF-8 bytes; "hex:" followed by hex
	// digits is a raw byte sequence.
	MustNotContain []string `json:"must_not_contain"`
	// MustContain is optional: byte strings or part names that must survive
	// in the output (same encoding as MustNotContain).
	MustContain []string `json:"must_contain,omitempty"`
}

type Entry struct {
	Path        string   `json:"path"`
	Family      string   `json:"family"`
	Description string   `json:"description"`
	Expected    Expected `json:"expected"`
}

// corpus collects generated files and manifest entries.
type corpus struct {
	root    string
	large   bool
	entries []Entry
}

func (c *corpus) add(path, family, desc string, data []byte, exp Expected) error {
	return c.addWith(path, family, desc, exp, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// addWith streams a sample to disk through w, for large outputs.
func (c *corpus) addWith(path, family, desc string, exp Expected, w func(*os.File) error) error {
	full := filepath.Join(c.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.Create(full)
	if err != nil {
		return err
	}
	if err := w(f); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if exp.MustRemove == nil {
		exp.MustRemove = []string{}
	}
	if exp.MustNotContain == nil {
		exp.MustNotContain = []string{}
	}
	c.entries = append(c.entries, Entry{Path: path, Family: family, Description: desc, Expected: exp})
	return nil
}

func (c *corpus) writeManifest() error {
	sort.Slice(c.entries, func(i, j int) bool { return c.entries[i].Path < c.entries[j].Path })
	b, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.root, "manifest.json"), append(b, '\n'), 0o644)
}

func generate(out string, large bool) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	c := &corpus{root: out, large: large}
	for _, g := range []func(*corpus) error{genImages, genOOXML, genArchives, genStructured} {
		if err := g(c); err != nil {
			return err
		}
	}
	return c.writeManifest()
}

var (
	ok    = Expected{Result: resUnchanged}
	sanit = func(remove []string, notContain ...string) Expected {
		return Expected{Result: resSanitized, MustRemove: remove, MustNotContain: notContain}
	}
	rejected = func(reason string) Expected { return Expected{Result: resRejected, Reason: reason} }
)
