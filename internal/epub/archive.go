package epub

import (
	"archive/zip"
	"hash/crc32"
	"io"
	"strings"
	"time"
)

// file is one archive member, held in memory. A book is small enough that
// holding the rendered files costs less than the complexity of streaming two
// passes over the output.
type file struct {
	name string
	data []byte
}

// writeArchive writes the mimetype entry and then files, in order.
//
// The mimetype entry has to be first, stored without compression, and carry no
// extra field. That rules out zip.Writer.Create (which announces a data
// descriptor) and FileHeader.SetModTime (which adds an extended-timestamp extra
// field), so the entry is written with CreateRaw and explicit sizes.
func writeArchive(dst io.Writer, files []file, modified time.Time) error {
	zw := zip.NewWriter(dst)

	mimetype := []byte(mediaTypeEPUB)
	raw := &zip.FileHeader{
		Name:               mimetypeName,
		Method:             zip.Store,
		CRC32:              crc32.ChecksumIEEE(mimetype),
		CompressedSize64:   uint64(len(mimetype)),
		UncompressedSize64: uint64(len(mimetype)),
	}
	w, err := zw.CreateRaw(raw)
	if err != nil {
		return err
	}
	if _, err := w.Write(mimetype); err != nil {
		return err
	}

	for _, f := range files {
		header := &zip.FileHeader{
			Name:     f.name,
			Method:   methodFor(f.name),
			Modified: modified,
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := w.Write(f.data); err != nil {
			return err
		}
	}

	return zw.Close()
}

// methodFor stores formats that are already compressed and deflates the rest.
// Re-deflating a JPEG costs CPU and buys nothing.
func methodFor(name string) uint16 {
	switch {
	case strings.HasSuffix(name, ".jpg"),
		strings.HasSuffix(name, ".jpeg"),
		strings.HasSuffix(name, ".png"),
		strings.HasSuffix(name, ".gif"),
		strings.HasSuffix(name, ".webp"):
		return zip.Store
	default:
		return zip.Deflate
	}
}
