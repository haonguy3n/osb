package device

import (
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

type UKIInputs struct {
	Kernel string
	Initrd string
	Stub   string
	// Shim means the distro's shim is the first stage, so the initial slot's UKI
	// goes where shim looks for its second stage instead of at the firmware's
	// default boot file, and osb's certificate is staged on the ESP for MOK.
	Shim bool
}

type UKIResult struct {
	Paths  []string
	Verity bool
}

func InstallUKIs(diskPath, arch string, entries []osbstar.BootEntry, in UKIInputs, keyPEM, certPEM []byte, sign bool) (UKIResult, error) {
	var res UKIResult
	if _, err := exec.LookPath("ukify"); err != nil {
		return res, fmt.Errorf("the uki bootloader needs ukify on the host PATH - install systemd-ukify and sbsigntool")
	}
	if _, err := exec.LookPath("mcopy"); err != nil {
		return res, fmt.Errorf("the uki bootloader needs mcopy on the host PATH - install mtools")
	}
	parts, err := ReadGPT(diskPath)
	if err != nil {
		return res, err
	}
	esp, err := FindESP(parts)
	if err != nil {
		return res, err
	}
	tmp, err := os.MkdirTemp("", "osb-uki-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmp)

	var keyPath, crtPath string
	if sign {
		keyPath = filepath.Join(tmp, "db.key")
		crtPath = filepath.Join(tmp, "db.crt")
		if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return res, err
		}
		if err := os.WriteFile(crtPath, certPEM, 0o600); err != nil {
			return res, err
		}
	}

	for _, e := range entries {
		cmdline := e.Cmdline
		if e.Hash != "" {
			data, err := FindPartition(parts, e.Root)
			if err != nil {
				return res, err
			}
			hash, err := FindPartition(parts, e.Hash)
			if err != nil {
				return res, err
			}
			v, err := ApplyVerityToDisk(diskPath, data.Offset, data.Size, hash.Offset, hash.Size)
			if err != nil {
				return res, fmt.Errorf("%s: %w", e.Root, err)
			}
			cmdline += " " + VerityCmdline(v, e.Hash)
			res.Verity = true
		}
		uki := filepath.Join(tmp, "uki-"+e.Slot+".efi")
		if err := buildUKI(in, cmdline, arch, keyPath, crtPath, uki); err != nil {
			return res, err
		}
		var dests []string
		if e.Slot != "" {
			dests = append(dests, ABSlotUKIPath(e.Slot))
		}
		if e.Initial {
			if in.Shim {
				// Shim loads a binary with this name from its own directory and
				// verifies it against db + MokList; that is the whole trust path.
				dests = append(dests, "/EFI/BOOT/"+shimLoaderName(arch))
			} else {
				dests = append(dests, "/EFI/BOOT/"+EFIBootName(arch))
			}
		}
		for _, d := range dests {
			if err := copyToFAT(diskPath, esp.Offset, uki, d); err != nil {
				return res, err
			}
			res.Paths = append(res.Paths, d)
		}
	}
	if in.Shim {
		if err := stageMOKCertificate(diskPath, esp.Offset, certPEM); err != nil {
			return res, err
		}
		res.Paths = append(res.Paths, "/EFI/osb/osb.crt")
	}
	return res, nil
}

// stageMOKCertificate puts osb's certificate on the ESP where MokManager and
// mokutil can reach it: DER for MokManager's "enroll key from disk" at the
// console, PEM for `mokutil --import` from a running system. It is public
// material - only the private key that signs the UKI matters.
func stageMOKCertificate(diskPath string, espOffset int64, certPEM []byte) error {
	dir, err := os.MkdirTemp("", "osb-mok-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("the Secure Boot certificate is not PEM encoded")
	}
	derPath := filepath.Join(dir, "osb.crt")
	pemPath := filepath.Join(dir, "osb.pem")
	if err := os.WriteFile(derPath, block.Bytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(pemPath, certPEM, 0o644); err != nil {
		return err
	}
	if err := copyToFAT(diskPath, espOffset, derPath, "/EFI/osb/osb.crt"); err != nil {
		return err
	}
	return copyToFAT(diskPath, espOffset, pemPath, "/EFI/osb/osb.pem")
}

func shimLoaderName(arch string) string {
	if arch == "arm64" {
		return "grubaa64.efi"
	}
	return "grubx64.efi"
}

func buildUKI(in UKIInputs, cmdline, arch, keyPath, crtPath, out string) error {
	args := []string{"build",
		"--linux=" + in.Kernel,
		"--cmdline=" + cmdline,
		"--efi-arch=" + efiArch(arch),
		"--output=" + out,
	}
	if in.Initrd != "" {
		args = append(args, "--initrd="+in.Initrd)
	}
	if in.Stub != "" {
		args = append(args, "--stub="+in.Stub)
	}
	if keyPath != "" {
		args = append(args, "--secureboot-private-key="+keyPath, "--secureboot-certificate="+crtPath)
	}
	if out, err := exec.Command("ukify", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("building Unified Kernel Image: %w\n%s", err, out)
	}
	return nil
}

func copyToFAT(img string, offset int64, src, dest string) error {
	fat := fmt.Sprintf("%s@@%d", img, offset)
	_ = exec.Command("mmd", "-D", "s", "-i", fat, "::"+filepath.Dir(dest)).Run()
	if out, err := exec.Command("mcopy", "-o", "-i", fat, src, "::"+dest).CombinedOutput(); err != nil {
		return fmt.Errorf("writing %s to the ESP: %w\n%s", dest, err, out)
	}
	return nil
}

func efiArch(arch string) string {
	if arch == "arm64" {
		return "aa64"
	}
	return "x64"
}

func EFIBootName(arch string) string {
	if arch == "arm64" {
		return "BOOTAA64.EFI"
	}
	return "BOOTX64.EFI"
}

func ABSlotUKIPath(slot string) string {
	return "/EFI/osb/" + slot + ".efi"
}
