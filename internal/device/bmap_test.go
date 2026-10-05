package device

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func writeSparse(t *testing.T, path string, totalSize int64, dataAt []int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(totalSize); err != nil {
		t.Fatal(err)
	}
	for _, off := range dataAt {
		if _, err := f.WriteAt([]byte("osb-test-payload"), off); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
}

func field(t *testing.T, doc, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<` + name + `> (.*?) </` + name + `>`).FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("no %s field in bmap:\n%s", name, doc)
	}
	return m[1]
}

func TestWriteBmap(t *testing.T) {
	tests := []struct {
		name          string
		totalSize     int64
		dataAt        []int64
		wantRanges    int
		wantMappedMax int64
	}{
		{
			name:          "two islands of data in a mostly-hole file",
			totalSize:     16 << 20,
			dataAt:        []int64{0, 8 << 20},
			wantRanges:    2,
			wantMappedMax: 64,
		},
		{
			name:          "single block at offset zero",
			totalSize:     1 << 20,
			dataAt:        []int64{0},
			wantRanges:    1,
			wantMappedMax: 32,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			img := filepath.Join(dir, "disk.img")
			writeSparse(t, img, tc.totalSize, tc.dataAt)

			bmapPath := BmapPathFor(img)
			mapped, total, err := WriteBmap(img, bmapPath)
			if err != nil {
				t.Fatal(err)
			}

			raw, err := os.ReadFile(bmapPath)
			if err != nil {
				t.Fatal(err)
			}
			doc := string(raw)

			if got := field(t, doc, "ImageSize"); got != strconv.FormatInt(tc.totalSize, 10) {
				t.Errorf("ImageSize = %s, want %d", got, tc.totalSize)
			}
			wantTotal := tc.totalSize / bmapBlockSize
			if total != wantTotal {
				t.Errorf("total blocks = %d, want %d", total, wantTotal)
			}
			if mapped == 0 {
				t.Error("mapped = 0, want the written data to be mapped")
			}
			if mapped > tc.wantMappedMax {
				t.Errorf("mapped = %d blocks, want <= %d (sparseness lost?)", mapped, tc.wantMappedMax)
			}
			if got := strings.Count(doc, "<Range "); got != tc.wantRanges {
				t.Errorf("got %d ranges, want %d:\n%s", got, tc.wantRanges, doc)
			}
			if strings.Contains(doc, bmapChecksumPlaceholder) {
				t.Error("BmapFileChecksum still holds the zero placeholder")
			}
		})
	}
}

func TestWriteBmapChecksumVerifies(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "disk.img")
	writeSparse(t, img, 4<<20, []int64{0, 2 << 20})

	bmapPath := BmapPathFor(img)
	if _, _, err := WriteBmap(img, bmapPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(bmapPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	embedded := field(t, doc, "BmapFileChecksum")
	blanked := strings.Replace(doc, embedded, bmapChecksumPlaceholder, 1)
	if got := sha256Hex(blanked); got != embedded {
		t.Errorf("checksum = %s, want %s", got, embedded)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
