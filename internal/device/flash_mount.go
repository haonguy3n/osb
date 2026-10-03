package device

import (
	"bufio"
	"os"
	"strings"
)

type MountedPartition struct {
	Source     string
	Mountpoint string
}

func MountedPartitionsFor(devicePath string) ([]MountedPartition, error) {
	return mountedPartitionsFor(devicePath, "/proc/self/mountinfo")
}

func mountedPartitionsFor(devicePath, mountInfoPath string) ([]MountedPartition, error) {
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []MountedPartition
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		dash := strings.Index(line, " - ")
		if dash < 0 {
			continue
		}
		head := strings.Fields(line[:dash])
		tail := strings.Fields(line[dash+3:])
		if len(head) < 5 || len(tail) < 2 {
			continue
		}
		mountpoint := head[4]
		source := tail[1]

		if !sourceMatchesDisk(source, devicePath) {
			continue
		}
		key := source + "\x00" + mountpoint
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, MountedPartition{Source: source, Mountpoint: mountpoint})
	}
	return out, scanner.Err()
}

func sourceMatchesDisk(source, devicePath string) bool {
	if !strings.HasPrefix(source, "/dev/") {
		return false
	}
	if source == devicePath {
		return true
	}
	if !strings.HasPrefix(source, devicePath) {
		return false
	}
	suffix := source[len(devicePath):]
	if suffix == "" {
		return true
	}
	if suffix[0] == 'p' {
		suffix = suffix[1:]
	}
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
