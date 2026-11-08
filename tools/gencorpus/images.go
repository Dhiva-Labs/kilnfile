package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// testImage is a deterministic gradient with a bright corner marker, so
// rotation is visible.
func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x + y) * 255 / (w + h)), 255})
		}
	}
	for y := 0; y < h/4+1 && y < h; y++ {
		for x := 0; x < w/4+1 && x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	return img
}

func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, testImage(w, h)); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func jpegBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, testImage(w, h), &jpeg.Options{Quality: 80}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func gifBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := gif.Encode(&b, testImage(w, h), &gif.Options{NumColors: 32}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// bmpBytes writes an uncompressed 24-bit BMP.
func bmpBytes(w, h int) []byte {
	img := testImage(w, h)
	rowSize := (w*3 + 3) &^ 3
	size := 54 + rowSize*h
	b := make([]byte, 54, size)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[2:], uint32(size))
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	binary.LittleEndian.PutUint32(b[34:], uint32(rowSize*h))
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, rowSize)
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			row[x*3], row[x*3+1], row[x*3+2] = c.B, c.G, c.R
		}
		b = append(b, row...)
	}
	return b
}

// webpBytes is a minimal valid 1x1 lossless (VP8L) WebP.
func webpBytes() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 0x1a, 0, 0, 0, 'W', 'E', 'B', 'P',
		'V', 'P', '8', 'L', 0x0d, 0, 0, 0,
		0x2f, 0, 0, 0, 0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe, 0x07, 0x00,
	}
}

func pngChunk(typ string, data []byte) []byte {
	b := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(b, uint32(len(data)))
	copy(b[4:], typ)
	b = append(b, data...)
	crc := crc32.ChecksumIEEE(b[4:])
	return binary.BigEndian.AppendUint32(b, crc)
}

// hugePNG declares w x h pixels but carries only a token amount of data.
func hugePNG(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit RGB
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(make([]byte, 16))
	zw.Close()
	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, pngChunk("IHDR", ihdr)...)
	out = append(out, pngChunk("IDAT", z.Bytes())...)
	return append(out, pngChunk("IEND", nil)...)
}

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	data     []byte // little-endian value bytes
}

// buildIFD serializes an IFD at absolute TIFF offset base. Values larger than
// 4 bytes are placed after the table.
func buildIFD(entries []ifdEntry, base uint32) []byte {
	tableLen := 2 + 12*len(entries) + 4
	table := make([]byte, 0, tableLen)
	table = binary.LittleEndian.AppendUint16(table, uint16(len(entries)))
	var extra []byte
	for _, e := range entries {
		table = binary.LittleEndian.AppendUint16(table, e.tag)
		table = binary.LittleEndian.AppendUint16(table, e.typ)
		table = binary.LittleEndian.AppendUint32(table, e.count)
		if len(e.data) <= 4 {
			v := make([]byte, 4)
			copy(v, e.data)
			table = append(table, v...)
		} else {
			table = binary.LittleEndian.AppendUint32(table, base+uint32(tableLen+len(extra)))
			extra = append(extra, e.data...)
			if len(extra)%2 == 1 {
				extra = append(extra, 0)
			}
		}
	}
	table = append(table, 0, 0, 0, 0) // no next IFD
	return append(table, extra...)
}

func ascii(s string) ifdEntry {
	return ifdEntry{typ: 2, count: uint32(len(s) + 1), data: append([]byte(s), 0)}
}

func rationals(vals ...[2]uint32) ifdEntry {
	var d []byte
	for _, v := range vals {
		d = binary.LittleEndian.AppendUint32(d, v[0])
		d = binary.LittleEndian.AppendUint32(d, v[1])
	}
	return ifdEntry{typ: 5, count: uint32(len(vals)), data: d}
}

func withTag(e ifdEntry, tag uint16) ifdEntry { e.tag = tag; return e }

// exifSegment builds a JPEG APP1 EXIF segment. model and gps are optional;
// orientation 0 omits the tag.
func exifSegment(model string, gps bool, orientation uint16) []byte {
	var entries []ifdEntry
	if model != "" {
		entries = append(entries, withTag(ascii("KilnCam"), 0x010F), withTag(ascii(model), 0x0110))
	}
	if orientation != 0 {
		entries = append(entries, ifdEntry{tag: 0x0112, typ: 3, count: 1, data: binary.LittleEndian.AppendUint16(nil, orientation)})
	}
	var gpsIFD []byte
	if gps {
		entries = append(entries, ifdEntry{tag: 0x8825, typ: 4, count: 1})
	}
	const base0 = 8
	ifd0 := buildIFD(entries, base0)
	if gps {
		gpsOff := uint32(base0 + len(ifd0))
		entries[len(entries)-1].data = binary.LittleEndian.AppendUint32(nil, gpsOff)
		ifd0 = buildIFD(entries, base0)
		gpsIFD = buildIFD([]ifdEntry{
			{tag: 0, typ: 1, count: 4, data: []byte{2, 3, 0, 0}},
			withTag(ascii("N"), 1),
			withTag(rationals([2]uint32{48, 1}, [2]uint32{51, 1}, [2]uint32{3000, 100}), 2),
			withTag(ascii("E"), 3),
			withTag(rationals([2]uint32{2, 1}, [2]uint32{17, 1}, [2]uint32{4000, 100}), 4),
		}, gpsOff)
	}
	tiff := append([]byte("II*\x00\x08\x00\x00\x00"), ifd0...)
	tiff = append(tiff, gpsIFD...)
	body := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(body)+2))
	return append(seg, body...)
}

// insertAfterSOI puts a segment right after the JPEG SOI marker.
func insertAfterSOI(jpg, seg []byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func genImages(c *corpus) error {
	hexExif := "hex:" + "457869660000" // "Exif\0\0"
	type s struct {
		path, desc string
		data       []byte
		exp        Expected
	}
	jpg := jpegBytes(16, 16)
	polyglot := append(append([]byte{}, jpg...), zipBytes([]zipFile{{name: "payload.txt", data: []byte("inert polyglot payload")}})...)
	pngFull := pngBytes(64, 64)
	samples := []s{
		{"images/benign.png", "Benign 16x16 PNG.", pngBytes(16, 16), ok},
		{"images/benign.jpg", "Benign 16x16 JPEG with no metadata.", jpg, ok},
		{"images/benign.gif", "Benign 16x16 single-frame GIF.", gifBytes(16, 16), ok},
		{"images/benign.webp", "Benign 1x1 lossless WebP.", webpBytes(), ok},
		{"images/benign.bmp", "Benign 8x8 24-bit BMP.", bmpBytes(8, 8), ok},
		{"images/exif_gps.jpg", "JPEG with an EXIF APP1 segment carrying GPS coordinates (48.85N 2.29E) and camera make/model KilnCam 9000.",
			insertAfterSOI(jpg, exifSegment("KilnCam 9000", true, 1)),
			sanit([]string{actMetadata}, hexExif, "KilnCam 9000", "KilnCam")},
		{"images/exif_orientation6.jpg", "8x4 JPEG with EXIF orientation 6 (rotate 90 CW). Output pixels must be rotated to 4x8 and the EXIF dropped.",
			insertAfterSOI(jpegBytes(8, 4), exifSegment("", false, 6)),
			sanit([]string{actMetadata}, hexExif)},
		{"images/huge_dimensions.png", "PNG whose IHDR declares 100000x100000 pixels with a token IDAT.", hugePNG(100000, 100000), rejected(rsnLimitExceeded)},
		{"images/truncated.png", "Valid 64x64 PNG cut off two thirds of the way through.", pngFull[:len(pngFull)*2/3], rejected(rsnUnparseable)},
		{"images/polyglot_jpeg_zip.jpg", "Valid JPEG with a ZIP archive appended after EOI.", polyglot,
			Expected{Result: resSanitized, MustRemove: []string{actTrailing}, MustNotContain: []string{"hex:504b0304", "payload.txt"}}},
	}
	for _, x := range samples {
		if err := c.add(x.path, "image", x.desc, x.data, x.exp); err != nil {
			return err
		}
	}
	return nil
}
