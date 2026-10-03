package device

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"
)

const sectorSize = 512

const espTypeGUID = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"

type GPTPartition struct {
	Name   string
	Type   string
	Offset int64
	Size   int64
}

func ReadGPT(path string) ([]GPTPartition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hdr := make([]byte, sectorSize)
	if _, err := f.ReadAt(hdr, sectorSize); err != nil {
		return nil, fmt.Errorf("reading GPT header: %w", err)
	}
	if string(hdr[:8]) != "EFI PART" {
		return nil, fmt.Errorf("%s has no GPT partition table", path)
	}
	entriesLBA := int64(binary.LittleEndian.Uint64(hdr[72:]))
	count := int(binary.LittleEndian.Uint32(hdr[80:]))
	size := int(binary.LittleEndian.Uint32(hdr[84:]))
	if size < 128 || count > 1024 {
		return nil, fmt.Errorf("%s: malformed GPT header", path)
	}
	table := make([]byte, count*size)
	if _, err := f.ReadAt(table, entriesLBA*sectorSize); err != nil {
		return nil, fmt.Errorf("reading GPT entries: %w", err)
	}
	var parts []GPTPartition
	for i := 0; i < count; i++ {
		e := table[i*size : (i+1)*size]
		typ := guidString(e[0:16])
		if typ == "00000000-0000-0000-0000-000000000000" {
			continue
		}
		first := int64(binary.LittleEndian.Uint64(e[32:]))
		last := int64(binary.LittleEndian.Uint64(e[40:]))
		parts = append(parts, GPTPartition{
			Name:   utf16Name(e[56:128]),
			Type:   typ,
			Offset: first * sectorSize,
			Size:   (last - first + 1) * sectorSize,
		})
	}
	return parts, nil
}

func FindPartition(parts []GPTPartition, name string) (GPTPartition, error) {
	for _, p := range parts {
		if p.Name == name {
			return p, nil
		}
	}
	return GPTPartition{}, fmt.Errorf("no partition named %q in the image", name)
}

func FindESP(parts []GPTPartition) (GPTPartition, error) {
	for _, p := range parts {
		if p.Type == espTypeGUID {
			return p, nil
		}
	}
	return GPTPartition{}, fmt.Errorf("image has no EFI System Partition")
}

func guidString(b []byte) string {
	return strings.ToUpper(fmt.Sprintf("%08x-%04x-%04x-%x-%x",
		binary.LittleEndian.Uint32(b[0:4]),
		binary.LittleEndian.Uint16(b[4:6]),
		binary.LittleEndian.Uint16(b[6:8]),
		b[8:10], b[10:16]))
}

func utf16Name(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}
