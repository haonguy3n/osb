package device

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Candidate struct {
	Path     string
	Size     int64
	Bus      string
	Vendor   string
	Model    string
	ReadOnly bool
}

func ListCandidates() ([]Candidate, error) {
	systemBlocked := map[string]bool{}
	if data, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, d := range systemDisks(string(data)) {
			systemBlocked[d] = true
		}
	}
	return listCandidates("/sys", systemBlocked)
}

func listCandidates(sysroot string, systemBlocked map[string]bool) ([]Candidate, error) {
	entries, err := os.ReadDir(filepath.Join(sysroot, "class", "block"))
	if err != nil {
		return nil, fmt.Errorf("read /sys/class/block: %w", err)
	}

	var out []Candidate
	for _, e := range entries {
		name := e.Name()
		if skipByName(name) {
			continue
		}
		blockDir := filepath.Join(sysroot, "class", "block", name)

		if _, err := os.Stat(filepath.Join(blockDir, "partition")); err == nil {
			continue
		}

		removable := readUint(filepath.Join(blockDir, "removable")) == 1
		ro := readUint(filepath.Join(blockDir, "ro")) == 1
		sectors := readInt64(filepath.Join(blockDir, "size"))
		bus := readBus(blockDir)

		if !(removable || bus == "usb" || bus == "mmc") {
			continue
		}
		if sectors == 0 {
			continue
		}
		if ro {
			continue
		}
		if systemBlocked["/dev/"+name] {
			continue
		}

		out = append(out, Candidate{
			Path:     "/dev/" + name,
			Size:     sectors * 512,
			Bus:      bus,
			Vendor:   strings.TrimSpace(readString(filepath.Join(blockDir, "device", "vendor"))),
			Model:    strings.TrimSpace(readString(filepath.Join(blockDir, "device", "model"))),
			ReadOnly: ro,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func skipByName(name string) bool {
	for _, prefix := range []string{"loop", "sr", "ram", "dm-", "md", "zram", "fd"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func readBus(blockDir string) string {
	dev, err := filepath.EvalSymlinks(filepath.Join(blockDir, "device"))
	if err != nil {
		return ""
	}
	for cur := dev; cur != "/" && cur != "."; cur = filepath.Dir(cur) {
		sub, err := os.Readlink(filepath.Join(cur, "subsystem"))
		if err != nil {
			continue
		}
		bus := filepath.Base(sub)
		switch bus {
		case "usb", "mmc", "scsi", "ata", "nvme", "sdio":
			return bus
		}
	}
	return ""
}

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readUint(path string) uint64 {
	s := strings.TrimSpace(readString(path))
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

func readInt64(path string) int64 {
	s := strings.TrimSpace(readString(path))
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func FormatSize(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
