// Package installer turns an install request into the exact sequence of
// commands that provisions a target disk, and runs it.
//
// The sequence is produced as data - a []Step of argv slices - rather than
// executed inline. That is the whole point of the split: an installer is the
// one program in osb that cannot be exercised in CI (it needs a spare disk to
// destroy), so the part that decides *what to do* is separated from the part
// that does it. Plan() is pure and fully unit-tested; Run() is a thin loop.
//
// A wrong argv here erases somebody's disk, so the tests assert on the exact
// commands rather than on "it didn't error".
package installer

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Layout constants. The ESP size matches the bundled UEFI machines (64 MiB);
// it holds a bootloader and possibly a UKI, never kernels, so it does not grow
// with the OS.
const (
	espSizeMiB   = 64
	espLabel     = "ESP"
	rootLabel    = "rootfs"
	cryptMapper  = "cryptroot"
	targetMount  = "/mnt/target"
	espMountName = "efi"
)

// Firmware selects the boot method the target will use.
type Firmware string

const (
	FirmwareUEFI Firmware = "uefi"
	FirmwareBIOS Firmware = "bios"
)

// Request is a fully-answered install: everything the TUI collected, or
// everything an unattended install read from a config file. Validate() is the
// single gate - Plan assumes a validated Request.
type Request struct {
	// Disk is the whole-disk device node to install onto, e.g. /dev/sda.
	// Everything on it is destroyed.
	Disk string

	Firmware Firmware

	// Encrypt wraps the root filesystem in LUKS2. Passphrase is required
	// when set and is never written to disk by the planner - it is fed to
	// cryptsetup on stdin.
	Encrypt    bool
	Passphrase string

	Hostname string

	// Username empty means "no unprivileged user"; root still gets
	// RootPassword. Both passwords are plaintext here and are hashed before
	// they reach /etc/shadow.
	Username     string
	UserPassword string
	RootPassword string

	// SecureBoot installs the signed UKI the image was built with instead of
	// a bootloader config. It does not enrol keys into firmware - the
	// machine must already trust the signing key.
	SecureBoot bool

	// SourceRoot is the live medium's root, the tree copied onto the target.
	SourceRoot string
}

// Step is one action in the install sequence: either a command to execute or
// a file to write. Both forms are data so the whole sequence can be asserted
// on in tests without touching a disk.
type Step struct {
	// Desc is shown in the progress UI.
	Desc string
	// Argv is executed directly - no shell, so no quoting bugs and no way
	// for a hostname or username to become a command.
	Argv []string
	// Stdin, when non-empty, is written to the process. Used for the LUKS
	// passphrase so it never appears in argv (and so never in /proc/*/cmdline
	// or a process listing).
	Stdin string

	// WritePath, when non-empty, makes this a file write instead of a
	// command: Content is written there with Mode. Generating fstab and
	// friends this way keeps them out of shell redirections, which is both
	// safer and far easier to assert on.
	WritePath string
	Content   string
	Mode      uint32
}

// IsWrite reports whether the step writes a file rather than running a command.
func (s Step) IsWrite() bool { return s.WritePath != "" }

var (
	ErrNoDisk      = errors.New("no target disk selected")
	ErrNoPassphi   = errors.New("encryption requested but no passphrase given")
	ErrNoHostname  = errors.New("no hostname given")
	ErrNoSource    = errors.New("no source root given")
	ErrNoPassword  = errors.New("no root password and no user account - the system would be unloginable")
	ErrBadHostname = errors.New("hostname must be 1-63 chars of [a-z0-9-] and not start or end with '-'")
	ErrBadUsername = errors.New("username must start with a lowercase letter and contain only [a-z0-9_-]")
	// A legacy-BIOS layout has no unencrypted partition to stage the boot
	// chain on, so encryption there produces an unbootable disk.
	ErrBIOSEncrypt    = errors.New("full-disk encryption requires UEFI: a BIOS layout has no ESP to hold the unencrypted kernel and bootloader")
	ErrBIOSSecureBoot = errors.New("secure boot requires UEFI")
)

// Validate rejects a Request that would produce a broken or unloginable
// system, or one whose free-form fields could break the files they are
// written into. It is deliberately strict: the installer runs unattended
// after the last confirmation screen.
func (r *Request) Validate() error {
	if r.Disk == "" {
		return ErrNoDisk
	}
	if r.SourceRoot == "" {
		return ErrNoSource
	}
	if r.Encrypt && r.Passphrase == "" {
		return ErrNoPassphi
	}
	if r.Hostname == "" {
		return ErrNoHostname
	}
	if !validHostname(r.Hostname) {
		return ErrBadHostname
	}
	if r.Username != "" && !validUsername(r.Username) {
		return ErrBadUsername
	}
	// A machine with neither a root password nor a user account cannot be
	// logged into at all. Catch it here rather than after the disk is wiped.
	if r.RootPassword == "" && r.Username == "" {
		return ErrNoPassword
	}
	if r.Firmware != FirmwareUEFI && r.Firmware != FirmwareBIOS {
		return fmt.Errorf("unknown firmware %q (want uefi or bios)", r.Firmware)
	}
	// A BIOS layout has no ESP, so limine's stage 2, its config, the kernel
	// and the initramfs would all have to live inside the LUKS container that
	// stage 1 cannot read - the machine would not boot. UEFI keeps them on
	// the unencrypted ESP instead.
	if r.Encrypt && r.Firmware == FirmwareBIOS {
		return ErrBIOSEncrypt
	}
	// Secure Boot means booting a signed UKI, which is an EFI application.
	if r.SecureBoot && r.Firmware == FirmwareBIOS {
		return ErrBIOSSecureBoot
	}
	return nil
}

func validHostname(h string) bool {
	if len(h) == 0 || len(h) > 63 {
		return false
	}
	if h[0] == '-' || h[len(h)-1] == '-' {
		return false
	}
	for _, c := range h {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func validUsername(u string) bool {
	if len(u) == 0 || len(u) > 32 {
		return false
	}
	if u[0] < 'a' || u[0] > 'z' {
		return false
	}
	for _, c := range u {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// PartitionDevice returns the device node for partition n of disk. NVMe and
// mmcblk devices insert a "p" before the partition number (/dev/nvme0n1p1)
// where SATA/USB do not (/dev/sda1); getting this wrong points mkfs at a
// device that does not exist, or worse, at a different disk.
func PartitionDevice(disk string, n int) string {
	base := filepath.Base(disk)
	if strings.HasPrefix(base, "nvme") || strings.HasPrefix(base, "mmcblk") ||
		strings.HasPrefix(base, "loop") || strings.HasPrefix(base, "md") {
		return fmt.Sprintf("%sp%d", disk, n)
	}
	return fmt.Sprintf("%s%d", disk, n)
}

// Plan builds the full install sequence for a validated Request.
//
// Layout, UEFI:   p1 ESP (FAT32, 64M)  p2 root (ext4, rest)
// Layout, BIOS:   p1 root (ext4, all)  - MBR, no ESP
//
// With Encrypt, the root partition holds a LUKS2 container and the ext4 lives
// on /dev/mapper/cryptroot.
func Plan(r Request) ([]Step, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	var steps []Step
	add := func(desc string, argv ...string) {
		steps = append(steps, Step{Desc: desc, Argv: argv})
	}

	uefi := r.Firmware == FirmwareUEFI

	// --- partition ---------------------------------------------------
	// sfdisk reads the layout on stdin. A whole-disk wipe first: sfdisk
	// happily writes a new table over an old one, but stale LUKS/ext4
	// superblocks in the *partitions* can confuse blkid and later mounts.
	add("Wiping existing signatures", "wipefs", "--all", r.Disk)

	var espDev, rootDev string
	if uefi {
		espDev = PartitionDevice(r.Disk, 1)
		rootDev = PartitionDevice(r.Disk, 2)
		steps = append(steps, Step{
			Desc: "Creating GPT partition table",
			Argv: []string{"sfdisk", "--label", "gpt", r.Disk},
			Stdin: fmt.Sprintf("size=%dMiB, type=uefi, name=%s\ntype=linux, name=%s\n",
				espSizeMiB, espLabel, rootLabel),
		})
	} else {
		rootDev = PartitionDevice(r.Disk, 1)
		steps = append(steps, Step{
			Desc:  "Creating MBR partition table",
			Argv:  []string{"sfdisk", "--label", "dos", r.Disk},
			Stdin: "type=83, bootable\n",
		})
	}
	// The kernel re-reads the table asynchronously; without settling, the
	// very next mkfs can race the partition nodes into existence.
	add("Waiting for partition nodes", "udevadm", "settle")

	// --- encryption --------------------------------------------------
	fsDev := rootDev
	if r.Encrypt {
		steps = append(steps,
			Step{
				Desc: "Creating LUKS2 container",
				// --batch-mode skips the interactive "YES" confirmation;
				// the user already confirmed on the summary screen.
				// The passphrase goes on stdin, never in argv.
				Argv:  []string{"cryptsetup", "luksFormat", "--type", "luks2", "--batch-mode", rootDev},
				Stdin: r.Passphrase,
			},
			Step{
				Desc:  "Opening LUKS container",
				Argv:  []string{"cryptsetup", "open", rootDev, cryptMapper},
				Stdin: r.Passphrase,
			},
		)
		fsDev = "/dev/mapper/" + cryptMapper
	}

	// --- filesystems -------------------------------------------------
	add("Formatting root filesystem", "mkfs.ext4", "-F", "-L", rootLabel, fsDev)
	if uefi {
		add("Formatting EFI system partition", "mkfs.vfat", "-F", "32", "-n", espLabel, espDev)
	}

	// --- mount + copy ------------------------------------------------
	// The live medium has no reason to ship /mnt/target, and mount fails on a
	// missing mountpoint.
	add("Creating target mountpoint", "mkdir", "-p", targetMount)
	add("Mounting target", "mount", fsDev, targetMount)
	if uefi {
		espMount := filepath.Join(targetMount, "boot", espMountName)
		add("Creating ESP mountpoint", "mkdir", "-p", espMount)
		add("Mounting ESP", "mount", espDev, espMount)
	}
	// -a preserves ownership, modes, symlinks and xattrs; -H/-A/-X keep
	// hardlinks and ACLs. -x stops rsync from descending into /proc, /sys,
	// /dev and the ESP mount we just made.
	add("Copying system to target", "rsync", "-aHAXx", "--info=progress2",
		strings.TrimSuffix(r.SourceRoot, "/")+"/", targetMount+"/")

	return append(steps, configureSteps(r, rootDev, fsDev, espDev, uefi)...), nil
}
