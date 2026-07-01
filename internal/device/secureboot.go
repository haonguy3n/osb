package device

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Embedded test-only Secure Boot keypair. See secureboot/README.md — this key
// is public in git and must never sign anything shipped to real hardware. It
// exists so `osb run --machine <secureboot>` can validate the UEFI Secure Boot
// trust chain under QEMU without any project-supplied key.
//
//go:embed secureboot/db.crt
var sbCert []byte

//go:embed secureboot/db.key
var sbKey []byte

// espHeaderOffset is the byte offset of the first (ESP) partition in a osb UEFI
// disk image. image.star lays the GPT header in the first 1 MiB and places
// partition 1 immediately after, so the ESP FAT filesystem always begins here.
// ponytail: fixed 1 MiB; if the UEFI layout ever puts a partition before the
// ESP, derive this from machine.Partitions instead.
const espHeaderOffset = 1 << 20

// secureBootTools are the host executables the Secure Boot run path shells out
// to. All ship in standard packages; the preflight names the missing one rather
// than failing deep in a pipe. ukify assembles and signs the Unified Kernel
// Image; mcopy installs it on the ESP; virt-fw-vars enrolls the key.
var secureBootTools = []string{"ukify", "mcopy", "virt-fw-vars"}

// checkSecureBootTools returns an error naming the first missing host tool the
// Secure Boot path needs, with the package that provides it.
func checkSecureBootTools() error {
	pkgHint := map[string]string{
		"ukify":        "systemd-ukify (Debian/Ubuntu), systemd-ukify (Fedora), systemd-boot (Arch)",
		"mcopy":        "mtools",
		"virt-fw-vars": "python3-virt-firmware (Debian/Ubuntu), virt-firmware (Fedora/Arch)",
	}
	for _, t := range secureBootTools {
		if _, err := exec.LookPath(t); err != nil {
			return fmt.Errorf("Secure Boot needs %q on the host PATH — install %s", t, pkgHint[t])
		}
	}
	return nil
}

// ovmfSecbootFirmware returns paths to a Secure-Boot-capable OVMF firmware
// split: the read-only CODE image and the pristine (setup-mode) VARS template
// osb enrolls its key into. Returns "","" if either is missing. Secure Boot
// requires the split CODE/VARS form (the combined single-file OVMF.fd used by
// the plain UEFI path cannot carry an enrolled, SMM-protected variable store).
//
// Candidates span the common packaging layouts:
//   - Debian/Ubuntu: ovmf → OVMF_CODE_4M.secboot.fd + OVMF_VARS_4M.fd
//   - Fedora/RHEL:   edk2-ovmf → OVMF_CODE.secboot.fd + OVMF_VARS.fd
//   - Arch:          edk2-ovmf → x64/OVMF_CODE.secboot.4m.fd + OVMF_VARS.4m.fd
func ovmfSecbootFirmware() (code, vars string) {
	type pair struct{ code, vars string }
	for _, p := range []pair{
		{"/usr/share/OVMF/OVMF_CODE_4M.secboot.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
		{"/usr/share/OVMF/OVMF_CODE.secboot.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
		{"/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
		{"/usr/share/edk2/x64/OVMF_CODE.secboot.fd", "/usr/share/edk2/x64/OVMF_VARS.fd"},
		{"/usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd", "/usr/share/edk2/x64/OVMF_VARS.4m.fd"},
		{"/usr/share/OVMF/x64/OVMF_CODE.secboot.4m.fd", "/usr/share/OVMF/x64/OVMF_VARS.4m.fd"},
	} {
		_, ec := os.Stat(p.code)
		_, ev := os.Stat(p.vars)
		if ec == nil && ev == nil {
			return p.code, p.vars
		}
	}
	return "", ""
}

// prepareSecureBoot produces the two run-time artifacts the Secure Boot QEMU
// launch needs, both written beside imgPath in the build dir and reused across
// runs:
//
//   - a signed disk (imgPath with ".sb" before the extension): a copy of the
//     built image whose ESP boots a signed Unified Kernel Image at
//     EFI/BOOT/BOOTX64.EFI — the kernel, initramfs, and command line assembled
//     into one PE binary and signed with the key. The firmware verifies and
//     runs it directly: no GRUB, no shim, so nothing gates the kernel under
//     Secure Boot. The canonical disk.img is never touched.
//   - an enrolled OVMF variable store (OVMF_VARS.sb.fd): the setup-mode VARS
//     template with the certificate enrolled as PK/KEK/db and Secure Boot
//     turned on, so the firmware enforces the signature.
//
// kernel is the kernel image and initrd the (optional) initramfs, both from the
// built rootfs; cmdline is the machine's kernel command line, embedded in the
// UKI. Returns the two paths. varsTemplate is the pristine setup-mode VARS from
// ovmfSecbootFirmware.
func prepareSecureBoot(imgPath, varsTemplate, kernel, initrd, cmdline string) (signedImg, enrolledVars string, err error) {
	dir := filepath.Dir(imgPath)
	if kernel == "" {
		return "", "", fmt.Errorf("Secure Boot: no kernel found in the built rootfs to build a Unified Kernel Image")
	}

	// Materialize the embedded keypair to a temp dir for the CLIs; it holds
	// the private key so it lives outside the build dir and is removed after.
	keyDir, err := os.MkdirTemp("", "osb-sb-keys-")
	if err != nil {
		return "", "", fmt.Errorf("temp key dir: %w", err)
	}
	defer os.RemoveAll(keyDir)
	crtPath := filepath.Join(keyDir, "db.crt")
	keyPath := filepath.Join(keyDir, "db.key")
	if err := os.WriteFile(crtPath, sbCert, 0o600); err != nil {
		return "", "", fmt.Errorf("writing cert: %w", err)
	}
	if err := os.WriteFile(keyPath, sbKey, 0o600); err != nil {
		return "", "", fmt.Errorf("writing key: %w", err)
	}

	// --- signed disk copy with the UKI installed on its ESP ---
	ext := filepath.Ext(imgPath)
	signedImg = imgPath[:len(imgPath)-len(ext)] + ".sb" + ext
	if err := copySparse(imgPath, signedImg); err != nil {
		return "", "", fmt.Errorf("copying image for signing: %w", err)
	}
	uki := filepath.Join(keyDir, "BOOTX64.EFI")
	if err := buildSignedUKI(kernel, initrd, cmdline, keyPath, crtPath, uki); err != nil {
		return "", "", err
	}
	if err := installUKIToESP(signedImg, uki); err != nil {
		return "", "", err
	}

	// --- enrolled variable store ---
	enrolledVars = filepath.Join(dir, "OVMF_VARS.sb.fd")
	// Enroll our test cert explicitly as PK, KEK, and db, and turn Secure Boot
	// on. All three are our single self-signed cert: db is the allow-list the
	// firmware verifies the bootloader against, PK/KEK root the store. We set
	// db directly rather than via --enroll-cert, whose --no-microsoft form
	// leaves db empty (only PK/KEK) — which the firmware reads as "nothing
	// trusted" and rejects even a correctly signed bootloader. sbOwnerGUID is
	// an arbitrary fixed owner GUID for the enrolled entries.
	cmd := exec.Command("virt-fw-vars",
		"--input", varsTemplate,
		"--set-pk", sbOwnerGUID, crtPath,
		"--add-kek", sbOwnerGUID, crtPath,
		"--add-db", sbOwnerGUID, crtPath,
		"--secure-boot",
		"--output", enrolledVars)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("enrolling Secure Boot keys into OVMF vars: %w\n%s", err, out)
	}
	return signedImg, enrolledVars, nil
}

// sbOwnerGUID is the EFI signature owner GUID stamped on osb's enrolled test
// key entries. Arbitrary but fixed, so enrolled stores are reproducible.
const sbOwnerGUID = "a0b1c2d3-e4f5-6789-abcd-ef0123456789"

// buildSignedUKI assembles a Unified Kernel Image from the kernel, optional
// initramfs, and command line, and signs it with the key/cert, writing the
// signed PE to outPath. ukify embeds the kernel (.linux), initramfs (.initrd),
// and cmdline (.cmdline) into one EFI executable on the systemd EFI stub, then
// runs sbsign — so the whole boot payload is a single signed artifact the
// firmware verifies against the enrolled db.
func buildSignedUKI(kernel, initrd, cmdline, keyPath, crtPath, outPath string) error {
	args := []string{"build",
		"--linux=" + kernel,
		"--cmdline=" + cmdline,
		"--secureboot-private-key=" + keyPath,
		"--secureboot-certificate=" + crtPath,
		"--output=" + outPath,
	}
	// Alpine boots through an initramfs (mkinitfs); embed it when present.
	if initrd != "" {
		args = append(args, "--initrd="+initrd)
	}
	if out, err := exec.Command("ukify", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("building signed Unified Kernel Image: %w\n%s", err, out)
	}
	return nil
}

// installUKIToESP writes the signed UKI over the default removable-media boot
// path (EFI/BOOT/BOOTX64.EFI) on the whole-disk image's ESP, replacing whatever
// bootloader the build put there. UEFI firmware loads and verifies this path
// with no boot entry required.
func installUKIToESP(img, uki string) error {
	espArg := fmt.Sprintf("%s@@%d", img, espHeaderOffset)
	if out, err := exec.Command("mcopy", "-o", "-i", espArg,
		uki, "::/EFI/BOOT/BOOTX64.EFI").CombinedOutput(); err != nil {
		return fmt.Errorf("writing UKI to ESP: %w\n%s", err, out)
	}
	return nil
}
