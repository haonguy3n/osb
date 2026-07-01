package device

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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

// SecureBootKeyMaterial returns the Secure Boot signing key and certificate osb
// should use for the project rooted at projectDir: the project-owned pair under
// keys/secureboot/db.{key,crt} when both are present, otherwise the embedded
// test key. isTest reports that the public, non-secret test key was used, so
// callers can refuse to sign real-hardware artifacts with it.
func SecureBootKeyMaterial(projectDir string) (keyPEM, certPEM []byte, isTest bool) {
	key, kerr := os.ReadFile(filepath.Join(projectDir, "keys", "secureboot", "db.key"))
	cert, cerr := os.ReadFile(filepath.Join(projectDir, "keys", "secureboot", "db.crt"))
	if kerr == nil && cerr == nil {
		return key, cert, false
	}
	return sbKey, sbCert, true
}

// GenerateSecureBootKey creates a self-signed Secure Boot signing keypair under
// projectDir/keys/secureboot (db.key + db.crt) with the given certificate
// common name. The pair signs Unified Kernel Images and is enrolled as
// PK/KEK/db. It refuses to overwrite an existing key so a project's signing key
// is never silently replaced. Returns the written key and cert paths.
func GenerateSecureBootKey(projectDir, commonName string) (keyPath, certPath string, err error) {
	dir := filepath.Join(projectDir, "keys", "secureboot")
	keyPath = filepath.Join(dir, "db.key")
	certPath = filepath.Join(dir, "db.crt")
	if _, statErr := os.Stat(keyPath); statErr == nil {
		return "", "", fmt.Errorf("Secure Boot key already exists at %s", keyPath)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}

	if err := writePEM(keyPath, 0o600, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv)); err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, 0o644, "CERTIFICATE", der); err != nil {
		return "", "", err
	}
	return keyPath, certPath, nil
}

// writePEM PEM-encodes block bytes of the given type to path with the given
// file mode.
func writePEM(path string, mode os.FileMode, blockType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// espHeaderOffset is the byte offset of the first (ESP) partition in a osb UEFI
// disk image. image.star lays the GPT header in the first 1 MiB and places
// partition 1 immediately after, so the ESP FAT filesystem always begins here.
// ponytail: fixed 1 MiB; if the UEFI layout ever puts a partition before the
// ESP, derive this from machine.Partitions instead.
const espHeaderOffset = 1 << 20

// secureBootToolHint names the package that provides each host tool the Secure
// Boot paths shell out to.
var secureBootToolHint = map[string]string{
	"ukify":        "systemd-ukify (Debian/Ubuntu, Fedora), systemd-boot (Arch)",
	"mcopy":        "mtools",
	"virt-fw-vars": "python3-virt-firmware (Debian/Ubuntu), virt-firmware (Fedora/Arch)",
}

// checkSecureBootTools returns an error naming the first of tools missing from
// the host PATH, with the package that provides it.
func checkSecureBootTools(tools ...string) error {
	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			return fmt.Errorf("Secure Boot needs %q on the host PATH — install %s", t, secureBootToolHint[t])
		}
	}
	return nil
}

// checkSecureBootBuildTools verifies the tools that sign a UKI into an image at
// build time (ukify assembles + signs, mcopy installs it on the ESP).
func checkSecureBootBuildTools() error { return checkSecureBootTools("ukify", "mcopy") }

// checkSecureBootRunTools verifies the tools that boot a signed image under
// enforced Secure Boot in QEMU (virt-fw-vars enrolls the key).
func checkSecureBootRunTools() error { return checkSecureBootTools("virt-fw-vars") }

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
func ovmfSecbootFirmware(arch string) (code, vars string) {
	type pair struct{ code, vars string }
	candidates := []pair{
		{"/usr/share/OVMF/OVMF_CODE_4M.secboot.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
		{"/usr/share/OVMF/OVMF_CODE.secboot.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
		{"/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
		{"/usr/share/edk2/x64/OVMF_CODE.secboot.fd", "/usr/share/edk2/x64/OVMF_VARS.fd"},
		{"/usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd", "/usr/share/edk2/x64/OVMF_VARS.4m.fd"},
		{"/usr/share/OVMF/x64/OVMF_CODE.secboot.4m.fd", "/usr/share/OVMF/x64/OVMF_VARS.4m.fd"},
	}
	if arch == "arm64" {
		candidates = []pair{
			{"/usr/share/AAVMF/AAVMF_CODE.secboot.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
			{"/usr/share/edk2/aarch64/QEMU_EFI.secboot.fd", "/usr/share/edk2/aarch64/vars-template-pflash.raw"},
			{"/usr/share/qemu-efi-aarch64/QEMU_EFI.secboot.fd", "/usr/share/qemu-efi-aarch64/vars-template-pflash.raw"},
		}
	}
	for _, p := range candidates {
		_, ec := os.Stat(p.code)
		_, ev := os.Stat(p.vars)
		if ec == nil && ev == nil {
			return p.code, p.vars
		}
	}
	return "", ""
}

// SignImageUKI signs a Unified Kernel Image into the ESP of the whole-disk image
// at diskPath, in place, at build time. It reads the kernel and initramfs from
// the image's unpacked rootfs (rootfs/boot beside diskPath), embeds them with
// cmdline into one PE, signs it with keyPEM/certPEM, and installs it at
// EFI/BOOT/BOOTX64.EFI — so the shipped image boots signed on real hardware,
// not only under QEMU. The firmware verifies and runs the UKI directly, with no
// GRUB or shim to gate the kernel.
func SignImageUKI(diskPath, cmdline, arch string, keyPEM, certPEM []byte) error {
	if err := checkSecureBootBuildTools(); err != nil {
		return err
	}
	kernel, initrd := findBootKernel(diskPath)
	if kernel == "" {
		return fmt.Errorf("Secure Boot: no kernel in the built rootfs to build a Unified Kernel Image")
	}

	keyDir, err := os.MkdirTemp("", "osb-sb-keys-")
	if err != nil {
		return fmt.Errorf("temp key dir: %w", err)
	}
	defer os.RemoveAll(keyDir)
	keyPath := filepath.Join(keyDir, "db.key")
	crtPath := filepath.Join(keyDir, "db.crt")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing key: %w", err)
	}
	if err := os.WriteFile(crtPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("writing cert: %w", err)
	}

	uki := filepath.Join(keyDir, efiBootName(arch))
	if err := buildSignedUKI(kernel, initrd, cmdline, keyPath, crtPath, uki, arch); err != nil {
		return err
	}
	return installUKIToESP(diskPath, uki, arch)
}

// EnrollSecureBootVars writes an OVMF variable store (OVMF_VARS.sb.fd beside the
// image, under dir) with certPEM enrolled as PK/KEK/db and Secure Boot turned
// on, so the firmware enforces the signature on a build-time-signed image. It
// sets db directly rather than via virt-fw-vars --enroll-cert, whose
// --no-microsoft form leaves db empty (only PK/KEK) — which the firmware reads
// as "nothing trusted" and rejects even a correctly signed bootloader. Returns
// the vars path. varsTemplate is the pristine setup-mode VARS.
func EnrollSecureBootVars(dir, varsTemplate string, certPEM []byte) (string, error) {
	certDir, err := os.MkdirTemp("", "osb-sb-cert-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(certDir)
	crtPath := filepath.Join(certDir, "db.crt")
	if err := os.WriteFile(crtPath, certPEM, 0o600); err != nil {
		return "", err
	}

	enrolledVars := filepath.Join(dir, "OVMF_VARS.sb.fd")
	cmd := exec.Command("virt-fw-vars",
		"--input", varsTemplate,
		"--set-pk", sbOwnerGUID, crtPath,
		"--add-kek", sbOwnerGUID, crtPath,
		"--add-db", sbOwnerGUID, crtPath,
		"--secure-boot",
		"--output", enrolledVars)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("enrolling Secure Boot keys into OVMF vars: %w\n%s", err, out)
	}
	return enrolledVars, nil
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
func buildSignedUKI(kernel, initrd, cmdline, keyPath, crtPath, outPath, arch string) error {
	args := []string{"build",
		"--linux=" + kernel,
		"--cmdline=" + cmdline,
		"--efi-arch=" + efiArch(arch),
		"--stub=" + ukiStub(arch),
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

// efiArch maps an osb arch to the systemd EFI architecture token ukify expects.
func efiArch(arch string) string {
	if arch == "arm64" {
		return "aa64"
	}
	return "x64"
}

// ukiStub returns the systemd EFI stub for the target arch. Explicitly naming it
// (rather than letting ukify guess from the kernel) makes cross-arch signing —
// e.g. an arm64 image built on an x86 host — reliable.
func ukiStub(arch string) string {
	if arch == "arm64" {
		return "/usr/lib/systemd/boot/efi/linuxaa64.efi.stub"
	}
	return "/usr/lib/systemd/boot/efi/linuxx64.efi.stub"
}

// efiBootName is the default removable-media boot path the firmware loads for
// the target arch: BOOTX64.EFI on x86_64, BOOTAA64.EFI on arm64.
func efiBootName(arch string) string {
	if arch == "arm64" {
		return "BOOTAA64.EFI"
	}
	return "BOOTX64.EFI"
}

// installUKIToESP writes the signed UKI over the default removable-media boot
// path (EFI/BOOT/BOOT<arch>.EFI) on the whole-disk image's ESP, replacing
// whatever bootloader the build put there. UEFI firmware loads and verifies this
// path with no boot entry required.
func installUKIToESP(img, uki, arch string) error {
	espArg := fmt.Sprintf("%s@@%d", img, espHeaderOffset)
	if out, err := exec.Command("mcopy", "-o", "-i", espArg,
		uki, "::/EFI/BOOT/"+efiBootName(arch)).CombinedOutput(); err != nil {
		return fmt.Errorf("writing UKI to ESP: %w\n%s", err, out)
	}
	return nil
}
