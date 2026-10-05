package device

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	bmapBlockSize           = 4096
	bmapChecksumPlaceholder = "0000000000000000000000000000000000000000000000000000000000000000"
)

type blockRange struct {
	first, last int64
	checksum    string
}

func WriteBmap(imgPath, bmapPath string) (mapped, total int64, err error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	size := info.Size()
	total = (size + bmapBlockSize - 1) / bmapBlockSize

	ranges, mapped, err := mappedRanges(f, size)
	if err != nil {
		return 0, 0, err
	}

	doc := renderBmap(size, total, mapped, ranges)
	sum := sha256.Sum256([]byte(doc))
	doc = strings.Replace(doc, bmapChecksumPlaceholder, hex.EncodeToString(sum[:]), 1)

	if err := os.WriteFile(bmapPath, []byte(doc), 0o644); err != nil {
		return 0, 0, err
	}
	return mapped, total, nil
}

func mappedRanges(f *os.File, size int64) ([]blockRange, int64, error) {
	var (
		ranges []blockRange
		mapped int64
		off    int64

		runStart, runEnd int64 = -1, -1
		runHash                = sha256.New()
	)

	closeRun := func() {
		if runStart < 0 {
			return
		}
		ranges = append(ranges, blockRange{
			first:    runStart,
			last:     runEnd,
			checksum: hex.EncodeToString(runHash.Sum(nil)),
		})
		mapped += runEnd - runStart + 1
		runStart, runEnd = -1, -1
		runHash.Reset()
	}

	fd := int(f.Fd())
	buf := make([]byte, bmapBlockSize)
	zero := make([]byte, bmapBlockSize)

	for off < size {
		start, err := unix.Seek(fd, off, unix.SEEK_DATA)
		if err != nil {
			if err == unix.ENXIO {
				break
			}
			return nil, 0, fmt.Errorf("SEEK_DATA at %d: %w", off, err)
		}
		end, err := unix.Seek(fd, start, unix.SEEK_HOLE)
		if err != nil {
			return nil, 0, fmt.Errorf("SEEK_HOLE at %d: %w", start, err)
		}
		if end > size {
			end = size
		}

		firstBlk := start / bmapBlockSize
		lastBlk := (end - 1) / bmapBlockSize

		for blk := firstBlk; blk <= lastBlk; blk++ {
			n, err := f.ReadAt(buf, blk*bmapBlockSize)
			if err != nil && err != io.EOF {
				return nil, 0, fmt.Errorf("reading block %d: %w", blk, err)
			}
			for i := n; i < bmapBlockSize; i++ {
				buf[i] = 0
			}

			if bytes.Equal(buf, zero) {
				closeRun()
				continue
			}
			if runStart < 0 {
				runStart = blk
			}
			runEnd = blk
			runHash.Write(buf)
		}
		closeRun()

		off = end
	}
	closeRun()
	return ranges, mapped, nil
}

func renderBmap(size, total, mapped int64, ranges []blockRange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" ?>\n")
	fmt.Fprintf(&b, "<bmap version=\"2.0\">\n")
	fmt.Fprintf(&b, "    <ImageSize> %d </ImageSize>\n", size)
	fmt.Fprintf(&b, "    <BlockSize> %d </BlockSize>\n", bmapBlockSize)
	fmt.Fprintf(&b, "    <BlocksCount> %d </BlocksCount>\n", total)
	fmt.Fprintf(&b, "    <MappedBlocksCount> %d </MappedBlocksCount>\n", mapped)
	fmt.Fprintf(&b, "    <ChecksumType> sha256 </ChecksumType>\n")
	fmt.Fprintf(&b, "    <BmapFileChecksum> %s </BmapFileChecksum>\n", bmapChecksumPlaceholder)
	fmt.Fprintf(&b, "    <BlockMap>\n")
	for _, r := range ranges {
		if r.first == r.last {
			fmt.Fprintf(&b, "        <Range chksum=\"%s\"> %d </Range>\n", r.checksum, r.first)
			continue
		}
		fmt.Fprintf(&b, "        <Range chksum=\"%s\"> %d-%d </Range>\n", r.checksum, r.first, r.last)
	}
	fmt.Fprintf(&b, "    </BlockMap>\n")
	fmt.Fprintf(&b, "</bmap>\n")
	return b.String()
}

func BmapPathFor(imgPath string) string {
	return filepath.Clean(imgPath) + ".bmap"
}
