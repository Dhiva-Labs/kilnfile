package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ulikunitz/xz"
)

var (
	sharedOnce sync.Once
	sharedDir  string
	sharedErr  error
)

// corpusDir generates the default corpus once and shares it between tests.
func corpusDir(t *testing.T) string {
	t.Helper()
	sharedOnce.Do(func() {
		sharedDir, sharedErr = os.MkdirTemp("", "gencorpus")
		if sharedErr == nil {
			sharedErr = generate(sharedDir, false)
		}
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	return sharedDir
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedDir != "" {
		os.RemoveAll(sharedDir)
	}
	os.Exit(code)
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func openZip(t *testing.T, name string) *zip.Reader {
	t.Helper()
	b := read(t, name)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		t.Fatalf("%s: %v", name, err)
	}
	return zr
}

func zipNames(zr *zip.Reader) []string {
	var n []string
	for _, f := range zr.File {
		n = append(n, f.Name)
	}
	return n
}

func readEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasEntry(zr *zip.Reader, name string) bool {
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

func wellFormed(b []byte) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		if _, err := d.Token(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

func TestManifest(t *testing.T) {
	dir := corpusDir(t)
	var entries []Entry
	if err := json.Unmarshal(read(t, "manifest.json"), &entries); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Path] {
			t.Errorf("duplicate path %s", e.Path)
		}
		seen[e.Path] = true
		if _, err := os.Stat(filepath.Join(dir, e.Path)); err != nil {
			t.Errorf("manifest path missing: %v", err)
		}
		switch e.Family {
		case "image", "ooxml", "archive", "structured":
			counts[e.Family]++
		default:
			t.Errorf("%s: bad family %q", e.Path, e.Family)
		}
		switch e.Expected.Result {
		case resSanitized, resUnchanged:
			if e.Expected.Reason != "" {
				t.Errorf("%s: reason set on non-rejected result", e.Path)
			}
		case resRejected:
			if e.Expected.Reason == "" {
				t.Errorf("%s: rejected without reason", e.Path)
			}
		default:
			t.Errorf("%s: bad result %q", e.Path, e.Expected.Result)
		}
		if e.Expected.Result == resSanitized && len(e.Expected.MustRemove) == 0 {
			t.Errorf("%s: sanitized without must_remove", e.Path)
		}
		if e.Description == "" {
			t.Errorf("%s: no description", e.Path)
		}
	}
	// Every file on disk except the manifest is listed.
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			rel = filepath.ToSlash(rel)
			if rel != "manifest.json" && !seen[rel] {
				t.Errorf("%s not in manifest", rel)
			}
		}
		return nil
	})
	t.Logf("samples per family: %v (total %d)", counts, len(entries))
}

func TestBenignImagesDecode(t *testing.T) {
	for _, c := range []struct{ name, format string }{
		{"images/benign.png", "png"}, {"images/benign.jpg", "jpeg"}, {"images/benign.gif", "gif"},
		{"images/exif_gps.jpg", "jpeg"}, {"images/exif_orientation6.jpg", "jpeg"},
	} {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(read(t, c.name)))
		if err != nil || format != c.format {
			t.Errorf("%s: format %q err %v", c.name, format, err)
		}
		if c.name == "images/exif_orientation6.jpg" && (cfg.Width != 8 || cfg.Height != 4) {
			t.Errorf("orientation sample is %dx%d, want 8x4", cfg.Width, cfg.Height)
		}
	}
	bmp := read(t, "images/benign.bmp")
	if string(bmp[:2]) != "BM" || len(bmp) != 54+24*8 {
		t.Errorf("bmp header/size wrong: %d bytes", len(bmp))
	}
	webp := read(t, "images/benign.webp")
	if string(webp[:4]) != "RIFF" || string(webp[8:12]) != "WEBP" || int(webp[4])+8 != len(webp) {
		t.Errorf("webp container wrong")
	}
}

func TestEXIF(t *testing.T) {
	g := read(t, "images/exif_gps.jpg")
	for _, s := range []string{"Exif\x00\x00", "KilnCam 9000", "II*\x00"} {
		if !bytes.Contains(g, []byte(s)) {
			t.Errorf("exif_gps.jpg lacks %q", s)
		}
	}
	if !bytes.HasPrefix(g, []byte{0xFF, 0xD8, 0xFF, 0xE1}) {
		t.Error("APP1 must directly follow SOI")
	}
	// GPS IFD pointer tag 0x8825 little-endian, and the N/E refs.
	if !bytes.Contains(g, []byte{0x25, 0x88, 0x04, 0x00}) {
		t.Error("no GPS IFD pointer")
	}
	// Orientation tag 0x0112 with value 6.
	o := read(t, "images/exif_orientation6.jpg")
	if !bytes.Contains(o, []byte{0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00}) {
		t.Error("orientation 6 not encoded")
	}
}

func TestHugeAndTruncatedPNG(t *testing.T) {
	cfg, err := png2config(read(t, "images/huge_dimensions.png"))
	if err != nil || cfg.Width != 100000 || cfg.Height != 100000 {
		t.Errorf("huge png config %v err %v", cfg, err)
	}
	trunc := read(t, "images/truncated.png")
	if _, _, err := image.Decode(bytes.NewReader(trunc)); err == nil {
		t.Error("truncated PNG decoded")
	}
	if _, err := png2config(trunc); err != nil {
		t.Errorf("truncated PNG header should still parse: %v", err)
	}
}

func png2config(b []byte) (image.Config, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	return cfg, err
}

func TestPolyglot(t *testing.T) {
	b := read(t, "images/polyglot_jpeg_zip.jpg")
	if !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		t.Error("no JPEG SOI")
	}
	i := bytes.Index(b, []byte("PK\x03\x04"))
	if i < 0 {
		t.Fatal("no ZIP signature")
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("polyglot is not a valid JPEG: %v", err)
	}
	if _, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Errorf("appended ZIP does not open: %v", err)
	}
}

func TestOOXMLValid(t *testing.T) {
	for _, n := range []string{"ooxml/benign.docx", "ooxml/benign.xlsx", "ooxml/benign.pptx", "ooxml/image.docx"} {
		zr := openZip(t, n)
		if len(zr.File) == 0 || zr.File[0].Name != "[Content_Types].xml" {
			t.Errorf("%s: [Content_Types].xml not first", n)
		}
		parts := map[string]bool{}
		for _, f := range zr.File {
			parts[f.Name] = true
			if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
				if err := wellFormed(readEntry(t, f)); err != nil {
					t.Errorf("%s: %s: %v", n, f.Name, err)
				}
			}
		}
		// Every internal relationship target must exist.
		for _, f := range zr.File {
			if !strings.HasSuffix(f.Name, ".rels") {
				continue
			}
			var r struct {
				R []struct {
					Target string `xml:"Target,attr"`
					Mode   string `xml:"TargetMode,attr"`
				} `xml:"Relationship"`
			}
			if err := xml.Unmarshal(readEntry(t, f), &r); err != nil {
				t.Fatal(err)
			}
			base := path.Dir(path.Dir(f.Name))
			for _, rl := range r.R {
				if rl.Mode == "External" {
					continue
				}
				if p := path.Join(base, rl.Target); !parts[p] {
					t.Errorf("%s: %s targets missing part %s", n, f.Name, p)
				}
			}
		}
	}
	// Image part is a real PNG.
	zr := openZip(t, "ooxml/image.docx")
	for _, f := range zr.File {
		if f.Name == "word/media/image1.png" {
			if _, _, err := image.Decode(bytes.NewReader(readEntry(t, f))); err != nil {
				t.Error(err)
			}
		}
	}
}

func entryText(t *testing.T, zr *zip.Reader, name string) string {
	for _, f := range zr.File {
		if f.Name == name {
			return string(readEntry(t, f))
		}
	}
	t.Fatalf("missing part %s", name)
	return ""
}

func TestOOXMLHostile(t *testing.T) {
	m := openZip(t, "ooxml/macro.docm")
	if !hasEntry(m, "word/vbaProject.bin") {
		t.Error("DOCM lacks vbaProject.bin")
	}
	ct := entryText(t, m, "[Content_Types].xml")
	if !strings.Contains(ct, "macroEnabled.main+xml") || !strings.Contains(ct, "vnd.ms-office.vbaProject") {
		t.Error("DOCM content types wrong")
	}
	if !strings.Contains(entryText(t, m, "word/_rels/document.xml.rels"), "relationships/vbaProject") {
		t.Error("DOCM lacks vba relationship")
	}
	for _, n := range []string{"ooxml/macro.docm", "ooxml/customui.docm"} {
		z := openZip(t, n)
		if b := entryText(t, z, "word/vbaProject.bin"); strings.Contains(b, "Sub ") || !strings.Contains(b, "DUMMY") {
			t.Errorf("%s: vbaProject.bin is not clearly dummy", n)
		}
	}
	cu := openZip(t, "ooxml/customui.docm")
	if !hasEntry(cu, "customUI/customUI.xml") {
		t.Error("no customUI part")
	}
	tp := openZip(t, "ooxml/external_template.docx")
	rels := entryText(t, tp, "word/_rels/settings.xml.rels")
	for _, s := range []string{"attachedTemplate", `TargetMode="External"`, "http://example.invalid/t.dotm"} {
		if !strings.Contains(rels, s) {
			t.Errorf("template rels lacks %s", s)
		}
	}
	if !strings.Contains(entryText(t, openZip(t, "ooxml/ddeauto.docx"), "word/document.xml"), `DDEAUTO c:\\windows\\system32\\cmd.exe "/k echo test"`) {
		t.Error("no DDEAUTO field")
	}
	ax := openZip(t, "ooxml/activex.docx")
	if !hasEntry(ax, "word/activeX/activeX1.xml") || !hasEntry(ax, "word/activeX/activeX1.bin") {
		t.Error("ActiveX parts missing")
	}
	ole := openZip(t, "ooxml/ole_object.docx")
	if !hasEntry(ole, "word/embeddings/oleObject1.bin") || !strings.Contains(entryText(t, ole, "word/document.xml"), "OLEObject") {
		t.Error("OLE parts missing")
	}
	xx := entryText(t, openZip(t, "ooxml/xxe_doctype.docx"), "word/document.xml")
	if !strings.Contains(xx, "<!DOCTYPE") || !strings.Contains(xx, `<!ENTITY xxe SYSTEM "file:///etc/passwd">`) {
		t.Error("XXE doctype missing")
	}
	// Every sample XML other than the XXE document is well formed.
	for _, n := range []string{"macro.docm", "customui.docm", "external_template.docx", "ddeauto.docx", "activex.docx", "ole_object.docx"} {
		z := openZip(t, "ooxml/"+n)
		for _, f := range z.File {
			if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
				if err := wellFormed(readEntry(t, f)); err != nil {
					t.Errorf("%s: %s: %v", n, f.Name, err)
				}
			}
		}
	}
}

func TestArchives(t *testing.T) {
	// Flat bomb: ratio and size.
	flat := openZip(t, "archives/zip_bomb_flat.zip")
	if len(flat.File) != 1 {
		t.Fatalf("flat bomb has %d entries", len(flat.File))
	}
	f := flat.File[0]
	if ratio := float64(f.UncompressedSize64) / float64(f.CompressedSize64); ratio < 500 {
		t.Errorf("flat bomb ratio %.0f, want >= 500", ratio)
	}
	if f.UncompressedSize64 < 10*mib {
		t.Errorf("flat bomb only %d bytes", f.UncompressedSize64)
	}
	// Nested bomb: depth beyond the default limit of 3.
	depth := 0
	cur := read(t, "archives/zip_bomb_nested.zip")
	for {
		zr, err := zip.NewReader(bytes.NewReader(cur), int64(len(cur)))
		if err != nil {
			t.Fatal(err)
		}
		depth++
		if len(zr.File) == 0 || !strings.HasSuffix(zr.File[0].Name, ".zip") {
			break
		}
		cur = readEntry(t, zr.File[0])
	}
	if depth <= 4 {
		t.Errorf("nested bomb depth %d, want > 4", depth)
	}
	// Traversal and symlink.
	want := map[string]string{"dotdot": "../../evil.txt", "absolute": "/etc/x", "drive": `C:\x`, "backslash": `a\..\..\b`}
	for k, name := range want {
		z := openZip(t, "archives/path_"+k+".zip")
		if !hasEntry(z, name) || !hasEntry(z, "ok.json") {
			t.Errorf("path_%s lacks %q (entries %q)", k, name, zipNames(z))
		}
	}
	sl := openZip(t, "archives/symlink.zip")
	found := false
	for _, f := range sl.File {
		if f.Name == "evil_link" && f.Mode()&fs.ModeSymlink != 0 {
			found = true
		}
	}
	if !found {
		t.Error("no symlink entry with symlink mode bits")
	}
	if n := len(openZip(t, "archives/zip_many_entries.zip").File); n != 20000 {
		t.Errorf("many-entries zip has %d entries", n)
	}
	// Benign zips open and their JSON parses.
	for _, n := range []string{"archives/benign.zip", "archives/benign_known_types.zip"} {
		z := openZip(t, n)
		if !json.Valid([]byte(entryText(t, z, "data.json"))) {
			t.Errorf("%s: data.json invalid", n)
		}
	}
}

func TestCompressedStreams(t *testing.T) {
	gz := func(name string) int64 {
		r, err := gzip.NewReader(bytes.NewReader(read(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, r)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := gz("archives/benign.gz"); n == 0 {
		t.Error("benign gzip empty")
	}
	bomb := read(t, "archives/gzip_bomb.gz")
	if ratio := gz("archives/gzip_bomb.gz") / int64(len(bomb)); ratio < 500 {
		t.Errorf("gzip bomb ratio %d", ratio)
	}
	xzLen := func(name string) int64 {
		r, err := xz.NewReader(bytes.NewReader(read(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, r)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if xzLen("archives/benign.xz") == 0 {
		t.Error("benign xz empty")
	}
	xb := read(t, "archives/xz_bomb.xz")
	if ratio := xzLen("archives/xz_bomb.xz") / int64(len(xb)); ratio < 500 {
		t.Errorf("xz bomb ratio %d", ratio)
	}
	// tar.gz: gzip around something that is a tar archive.
	r, _ := gzip.NewReader(bytes.NewReader(read(t, "archives/sample.tar.gz")))
	tb, _ := io.ReadAll(r)
	if len(tb) < 512 || string(tb[257:262]) != "ustar" {
		t.Error("tar.gz does not contain a tar")
	}
}

func jsonDepth(b []byte) int {
	d, max := 0, 0
	for _, c := range b {
		switch c {
		case '[', '{':
			d++
			if d > max {
				max = d
			}
		case ']', '}':
			d--
		}
	}
	return max
}

func TestStructured(t *testing.T) {
	if d := jsonDepth(read(t, "structured/deep.json")); d != 10000 {
		t.Errorf("deep.json depth %d", d)
	}
	for _, n := range []string{"structured/benign.json"} {
		if !json.Valid(read(t, n)) {
			t.Errorf("%s invalid", n)
		}
	}
	if json.Valid(read(t, "structured/malformed.json")) {
		t.Error("malformed.json is valid")
	}
	if err := wellFormed(read(t, "structured/benign.xml")); err != nil {
		t.Error(err)
	}
	yb := string(read(t, "structured/alias_bomb.yaml"))
	if strings.Count(yb, "&l") != 10 || strings.Count(yb, "*l") != 9*9 {
		t.Errorf("yaml bomb anchors/aliases wrong")
	}
	xb := string(read(t, "structured/billion_laughs.xml"))
	if strings.Count(xb, "<!ENTITY") != 10 || !strings.Contains(xb, "&lol8;&lol8;") {
		t.Error("xml bomb entity chain wrong")
	}
	if !strings.Contains(string(read(t, "structured/xxe.xml")), `SYSTEM "file:///etc/passwd"`) {
		t.Error("xxe.xml lacks external entity")
	}
	if !strings.Contains(string(read(t, "structured/processing_instruction.xml")), "<?xml-stylesheet") {
		t.Error("no PI")
	}
}

func TestBoundedOnDisk(t *testing.T) {
	var total int64
	filepath.WalkDir(corpusDir(t), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			fi, _ := d.Info()
			total += fi.Size()
			if fi.Size() > 4*mib {
				t.Errorf("%s is %d bytes", p, fi.Size())
			}
		}
		return nil
	})
	if total > 10*mib {
		t.Errorf("default corpus is %d bytes on disk", total)
	}
}

func hashTree(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	m := map[string][32]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		m[rel] = sha256.Sum256(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		if err := generate(d, false); err != nil {
			t.Fatal(err)
		}
	}
	ha, hb := hashTree(t, a), hashTree(t, b)
	if len(ha) == 0 || len(ha) != len(hb) {
		t.Fatalf("file counts differ: %d vs %d", len(ha), len(hb))
	}
	for k, v := range ha {
		if hb[k] != v {
			t.Errorf("%s differs between runs", k)
		}
	}
}
