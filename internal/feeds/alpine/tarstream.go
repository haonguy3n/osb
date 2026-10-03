package alpine

import (
	"archive/tar"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

func extractAPKINDEXFromStream(streamBytes []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(streamBytes))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("tar entry: %w", err)
		}
		if hdr.Name != "APKINDEX" {
			continue
		}
		return io.ReadAll(tr)
	}
}

type gzipBound [2]int

func gzipStreamBoundaries(data []byte) ([]gzipBound, error) {
	var out []gzipBound
	pos := 0
	for pos < len(data) {
		if pos+10 > len(data) || data[pos] != 0x1f || data[pos+1] != 0x8b {
			break
		}
		start := pos
		flg := data[pos+3]
		hdrEnd := pos + 10
		if flg&0x04 != 0 {
			if hdrEnd+2 > len(data) {
				return nil, fmt.Errorf("truncated FEXTRA")
			}
			xlen := int(binary.LittleEndian.Uint16(data[hdrEnd : hdrEnd+2]))
			hdrEnd += 2 + xlen
		}
		if flg&0x08 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x10 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x02 != 0 {
			hdrEnd += 2
		}
		if hdrEnd > len(data) {
			return nil, fmt.Errorf("truncated gzip header")
		}
		br := bytes.NewReader(data[hdrEnd:])
		zr := flate.NewReader(br)
		if _, err := io.Copy(io.Discard, zr); err != nil {
			zr.Close()
			return nil, fmt.Errorf("deflate stream %d: %w", len(out), err)
		}
		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("deflate close stream %d: %w", len(out), err)
		}
		deflateConsumed := (len(data) - hdrEnd) - br.Len()
		end := hdrEnd + deflateConsumed + 8
		if end > len(data) {
			return nil, fmt.Errorf("truncated gzip trailer")
		}
		out = append(out, gzipBound{start, end})
		pos = end
	}
	return out, nil
}
