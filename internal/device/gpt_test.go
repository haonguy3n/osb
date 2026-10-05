package device

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func putGUID(b []byte, s string) {
	var raw [16]byte
	hex := func(x string) []byte {
		out := make([]byte, len(x)/2)
		for i := range out {
			var v byte
			for _, c := range x[2*i : 2*i+2] {
				v <<= 4
				switch {
				case c >= '0' && c <= '9':
					v |= byte(c - '0')
				default:
					v |= byte(c-'A') + 10
				}
			}
			out[i] = v
		}
		return out
	}
	h := hex(s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36])
	binary.LittleEndian.PutUint32(raw[0:], binary.BigEndian.Uint32(h[0:4]))
	binary.LittleEndian.PutUint16(raw[4:], binary.BigEndian.Uint16(h[4:6]))
	binary.LittleEndian.PutUint16(raw[6:], binary.BigEndian.Uint16(h[6:8]))
	copy(raw[8:], h[8:16])
	copy(b, raw[:])
}

func TestReadGPT(t *testing.T) {
	img := make([]byte, 64*sectorSize)
	hdr := img[sectorSize:]
	copy(hdr, "EFI PART")
	binary.LittleEndian.PutUint64(hdr[72:], 2)
	binary.LittleEndian.PutUint32(hdr[80:], 4)
	binary.LittleEndian.PutUint32(hdr[84:], 128)
	entry := func(i int, typ string, first, last uint64, name string) {
		e := img[2*sectorSize+i*128:]
		putGUID(e[0:], typ)
		binary.LittleEndian.PutUint64(e[32:], first)
		binary.LittleEndian.PutUint64(e[40:], last)
		for j, c := range utf16.Encode([]rune(name)) {
			binary.LittleEndian.PutUint16(e[56+2*j:], c)
		}
	}
	entry(0, espTypeGUID, 34, 41, "esp")
	entry(1, "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709", 42, 63, "root-a")
	path := filepath.Join(t.TempDir(), "d.img")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	parts, err := ReadGPT(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partitions", len(parts))
	}
	esp, err := FindESP(parts)
	if err != nil || esp.Name != "esp" || esp.Offset != 34*sectorSize || esp.Size != 8*sectorSize {
		t.Fatalf("esp = %+v, %v", esp, err)
	}
	root, err := FindPartition(parts, "root-a")
	if err != nil || root.Offset != 42*sectorSize {
		t.Fatalf("root = %+v, %v", root, err)
	}
	if _, err := FindPartition(parts, "nope"); err == nil {
		t.Fatal("expected an error for a missing partition")
	}
}

func TestReadGPTRejectsMBR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.img")
	if err := os.WriteFile(path, make([]byte, 4*sectorSize), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGPT(path); err == nil {
		t.Fatal("expected an error for an image without GPT")
	}
}
