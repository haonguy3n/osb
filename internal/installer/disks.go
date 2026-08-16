package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Disk is a candidate install target.
type Disk struct {
	// Name is the kernel name, e.g. "sda" or "nvme0n1".
	Name string
	// Path is the device node, e.g. "/dev/sda".
	Path string
	// SizeBytes is the capacity as reported by the kernel.
	SizeBytes uint64
	// Model is the vendor string, empty for virtual devices.
	Model string
	// Removable marks USB sticks and card readers - usually the medium the
	// installer itself booted from, so it is shown but never preselected.
	Removable bool
}

// Size renders the capacity in the units a human picking a disk thinks in.
func (d Disk) Size() string {
	const unit = 1000.0
	v := float64(d.SizeBytes)
	for _, suffix := range []string{"B", "kB", "MB", "GB", "TB"} {
		if v < unit {
			return fmt.Sprintf("%.0f %s", v, suffix)
		}
		v /= unit
	}
	return fmt.Sprintf("%.1f PB", v)
}

func (d Disk) String() string {
	s := fmt.Sprintf("%-12s %8s", d.Path, d.Size())
	if d.Model != "" {
		s += "  " + d.Model
	}
	if d.Removable {
		s += "  [removable]"
	}
	return s
}

// sysBlock is the sysfs root, overridable in tests.
var sysBlock = "/sys/block"

// ListDisks returns the whole disks that can be installed onto, largest last.
//
// Partitions, loop devices, ram disks, and read-only devices (notably the
// squashfs loop of a live medium) are excluded: offering them as targets is
// how an installer ends up formatting the stick it is running from.
func ListDisks() ([]Disk, error) {
	entries, err := os.ReadDir(sysBlock)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", sysBlock, err)
	}

	var disks []Disk
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "loop"),
			strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "zram"),
			strings.HasPrefix(name, "sr"),
			strings.HasPrefix(name, "dm-"),
			strings.HasPrefix(name, "md"):
			continue
		}
		dir := filepath.Join(sysBlock, name)

		// size is in 512-byte sectors regardless of the device's own block
		// size - a kernel ABI quirk that is easy to get wrong.
		sectors, err := readUint(filepath.Join(dir, "size"))
		if err != nil || sectors == 0 {
			continue
		}
		if ro, _ := readUint(filepath.Join(dir, "ro")); ro == 1 {
			continue
		}
		removable, _ := readUint(filepath.Join(dir, "removable"))

		disks = append(disks, Disk{
			Name:      name,
			Path:      "/dev/" + name,
			SizeBytes: sectors * 512,
			Model:     readTrimmed(filepath.Join(dir, "device", "model")),
			Removable: removable == 1,
		})
	}

	sort.Slice(disks, func(i, j int) bool {
		if disks[i].SizeBytes != disks[j].SizeBytes {
			return disks[i].SizeBytes < disks[j].SizeBytes
		}
		return disks[i].Name < disks[j].Name
	})
	return disks, nil
}

// FirmwareOfHost reports how this machine booted. The installer defaults to
// matching it: installing a BIOS layout from a UEFI-booted live medium
// produces a disk the machine will not boot.
func FirmwareOfHost() Firmware {
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		return FirmwareUEFI
	}
	return FirmwareBIOS
}

func readUint(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
