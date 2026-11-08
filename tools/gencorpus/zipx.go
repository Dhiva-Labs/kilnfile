package main

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"time"
)

// fixedTime stamps every generated archive entry so output is reproducible.
var fixedTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

type zipFile struct {
	name  string
	data  []byte
	mode  fs.FileMode // 0 means a regular file with the default mode
	store bool        // use Store instead of Deflate
}

func writeZip(w io.Writer, files []zipFile) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: fixedTime}
		if f.store {
			h.Method = zip.Store
		}
		if f.mode != 0 {
			h.SetMode(f.mode)
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func zipBytes(files []zipFile) []byte {
	var b bytes.Buffer
	if err := writeZip(&b, files); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func zxWriter(w io.Writer) *zip.Writer { return zip.NewWriter(w) }

func zipHeader(name string) *zip.FileHeader {
	return &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedTime}
}
