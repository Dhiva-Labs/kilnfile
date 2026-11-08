package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/ulikunitz/xz"
)

const (
	mib = 1 << 20
	gib = 1 << 30
)

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func writeZeros(w io.Writer, n int64) error {
	_, err := io.CopyN(w, zeroReader{}, n)
	return err
}

// zeroZip is a single-entry deflate ZIP holding n zero bytes.
func zeroZip(w io.Writer, name string, n int64) error {
	zw := zxWriter(w)
	fw, err := zw.CreateHeader(zipHeader(name))
	if err != nil {
		return err
	}
	if err := writeZeros(fw, n); err != nil {
		return err
	}
	return zw.Close()
}

func gzipBytes(data []byte) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write(data)
	zw.Close()
	return b.Bytes()
}

func xzBytes(data []byte) []byte {
	var b bytes.Buffer
	xw, err := xz.NewWriter(&b)
	if err != nil {
		panic(err)
	}
	xw.Write(data)
	xw.Close()
	return b.Bytes()
}

func tarGz() []byte {
	var t bytes.Buffer
	tw := tar.NewWriter(&t)
	body := []byte("Hello from kilnfile\n")
	tw.WriteHeader(&tar.Header{Name: "hello.txt", Mode: 0o644, Size: int64(len(body)), ModTime: fixedTime, Format: tar.FormatUSTAR})
	tw.Write(body)
	tw.Close()
	return gzipBytes(t.Bytes())
}

func genArchives(c *corpus) error {
	flat, nestedInner, nestedLevels, gzN, xzN := int64(16*mib), int64(1*mib), 6, int64(16*mib), int64(16*mib)
	if c.large {
		flat, nestedInner, nestedLevels, gzN, xzN = 2*gib, 256*mib, 8, 2*gib, 2*gib
	}
	jsonDoc := []byte(`{"name":"kilnfile","ok":true}` + "\n")
	png := pngBytes(16, 16)

	benign := zipBytes([]zipFile{
		{name: "hello.txt", data: []byte("Hello from kilnfile\n")},
		{name: "data.json", data: jsonDoc},
		{name: "image.png", data: png},
	})
	benignKnown := zipBytes([]zipFile{
		{name: "data.json", data: jsonDoc},
		{name: "docs/image.png", data: png},
		{name: "docs/note.xml", data: []byte("<note>hi</note>\n")},
	})
	if err := c.add("archives/benign.zip", "archive", "ZIP of a text file, a PNG and a JSON file. The text entry has no pipeline (unknown type) and is removed by default.", benign,
		Expected{Result: resSanitized, MustRemove: []string{rsnUnknownType}, MustNotContain: []string{"hello.txt"}, MustContain: []string{"data.json", "image.png"}}); err != nil {
		return err
	}
	if err := c.add("archives/benign_known_types.zip", "archive", "ZIP containing only entries with pipelines (JSON, PNG, XML), including a subdirectory.", benignKnown,
		Expected{Result: resUnchanged, MustContain: []string{"data.json", "docs/image.png", "docs/note.xml"}}); err != nil {
		return err
	}

	if err := c.addWith("archives/zip_bomb_flat.zip", "archive",
		fmt.Sprintf("Flat ZIP bomb: one deflate entry of %d MiB of zeros (ratio over 1000).", flat/mib),
		rejected(rsnLimitExceeded), func(f *os.File) error { return zeroZip(f, "zeros.bin", flat) }); err != nil {
		return err
	}

	var inner bytes.Buffer
	if err := zeroZip(&inner, "zeros.bin", nestedInner); err != nil {
		return err
	}
	layer := inner.Bytes()
	for i := 0; i < nestedLevels; i++ {
		layer = zipBytes([]zipFile{{name: "a.zip", data: layer}, {name: "b.zip", data: layer}})
	}
	if err := c.add("archives/zip_bomb_nested.zip", "archive",
		fmt.Sprintf("Nested ZIP bomb: %d levels of ZIPs (default limit 3), two copies per level, innermost holds %d MiB of zeros.", nestedLevels, nestedInner/mib),
		layer, rejected(rsnLimitExceeded)); err != nil {
		return err
	}

	traversal := []struct{ file, name, desc string }{
		{"dotdot", "../../evil.txt", "relative parent traversal"},
		{"absolute", "/etc/x", "absolute path"},
		{"drive", `C:\x`, "Windows drive letter"},
		{"backslash", `a\..\..\b`, "backslash traversal"},
	}
	for _, t := range traversal {
		data := zipBytes([]zipFile{
			{name: "ok.json", data: jsonDoc},
			{name: t.name, data: []byte("inert test content")},
		})
		if err := c.add("archives/path_"+t.file+".zip", "archive",
			fmt.Sprintf("ZIP with a benign ok.json and an entry named %q (%s). The unsafe entry is rejected, the rest kept.", t.name, t.desc), data,
			Expected{Result: resSanitized, MustRemove: []string{actUnsafePath}, MustNotContain: []string{t.name}, MustContain: []string{"ok.json"}}); err != nil {
			return err
		}
	}

	link := zipBytes([]zipFile{
		{name: "ok.json", data: jsonDoc},
		{name: "evil_link", data: []byte("/etc/passwd"), mode: os.ModeSymlink | 0o777},
	})
	if err := c.add("archives/symlink.zip", "archive", "ZIP with a benign ok.json and a symlink entry (Unix mode bits) named evil_link.", link,
		Expected{Result: resSanitized, MustRemove: []string{actSymlink}, MustNotContain: []string{"evil_link"}, MustContain: []string{"ok.json"}}); err != nil {
		return err
	}

	many := make([]zipFile, 20000)
	for i := range many {
		many[i] = zipFile{name: fmt.Sprintf("many/%05d.txt", i), data: []byte("x"), store: true}
	}
	if err := c.add("archives/zip_many_entries.zip", "archive", "ZIP with 20000 one-byte entries (default limit 10000).", zipBytes(many), rejected(rsnLimitExceeded)); err != nil {
		return err
	}

	if err := c.add("archives/benign.gz", "archive", "Benign gzip of a small JSON document.", gzipBytes(jsonDoc),
		Expected{Result: resUnchanged}); err != nil {
		return err
	}
	if err := c.addWith("archives/gzip_bomb.gz", "archive",
		fmt.Sprintf("gzip bomb: %d MiB of zeros (ratio about 1000).", gzN/mib), rejected(rsnLimitExceeded),
		func(f *os.File) error {
			zw := gzip.NewWriter(f)
			if err := writeZeros(zw, gzN); err != nil {
				return err
			}
			return zw.Close()
		}); err != nil {
		return err
	}
	if err := c.add("archives/benign.xz", "archive", "Benign xz of a small JSON document.", xzBytes(jsonDoc),
		Expected{Result: resUnchanged}); err != nil {
		return err
	}
	if err := c.addWith("archives/xz_bomb.xz", "archive",
		fmt.Sprintf("xz bomb: %d MiB of zeros.", xzN/mib), rejected(rsnLimitExceeded),
		func(f *os.File) error {
			xw, err := xz.NewWriter(f)
			if err != nil {
				return err
			}
			if err := writeZeros(xw, xzN); err != nil {
				return err
			}
			return xw.Close()
		}); err != nil {
		return err
	}
	return c.add("archives/sample.tar.gz", "archive", "gzip-compressed tar. There is no tar parser, so the decompressed content is an unknown type.", tarGz(),
		rejected(rsnUnknownType))
}
