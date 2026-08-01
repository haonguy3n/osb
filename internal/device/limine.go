package device

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// Secure Boot with limine, as upstream defines it, is two things at once:
//
//  1. limine's own BOOTX64.EFI is signed with a key the firmware trusts, and
//  2. the BLAKE2B hash of limine.conf is *enrolled into that binary*.
//
// Only the second switches enforcement on. Upstream is explicit that a signed
// but unenrolled limine "treats Secure Boot as inactive" and provides "no
// integrity guarantees beyond those of the firmware itself" — it would sit in
// a verified chain and then load whatever kernel the config named. So osb
// always does both, and the config it enrols pins every file it loads by hash:
//
//	config hash enrolled in the signed binary
//	  -> limine.conf is verified on every boot
//	     -> kernel and initramfs paths carry #<blake2b>
//	        -> limine panics if either file changed
//
// That closes the chain from firmware to userspace without the systemd EFI
// stub, which is the alternative osb uses for its UKI machines.

// limineB2SumMarker is the anchor `limine enroll-config` looks for: the 26
// bytes are followed by 128 hex characters holding the enrolled config hash,
// all '0' in an unenrolled binary. Patching those bytes is the entirety of
// what enroll-config does, so osb does it directly rather than shelling out to
// a target-arch, musl-linked tool that will not run on the build host.
const limineB2SumMarker = "++CONFIG_B2SUM_SIGNATURE++"

// limineHashHexLen is the length of a BLAKE2B-512 digest in hex. limine
// hard-requires exactly this many characters.
const limineHashHexLen = 128

// LimineESPConfigPath is where the disk task and this signer agree the config
// lives — the ESP root, the last of limine's four search locations and the
// only one needing no extra directory.
const LimineESPConfigPath = "/limine.conf"

// Blake2bFile returns the BLAKE2B-512 digest of a file as lowercase hex.
//
// limine hashes the bytes as they sit on disk — its filter chain is
// raw -> blake2b -> gzip, so a compressed kernel is hashed compressed. Hashing
// the file verbatim is therefore correct and no decompression is involved.
func Blake2bFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Blake2bBytes returns the BLAKE2B-512 digest of in-memory content as hex.
// The config is hashed before it is written anywhere, so it never has to be
// read back off disk to be enrolled.
func Blake2bBytes(b []byte) (string, error) {
	h, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EnrollLimineConfigHash writes hashHex into the limine EFI binary at efiPath,
// the operation `limine enroll-config` performs.
//
// The binary must be patched *before* it is signed: the enrolled hash is part
// of the PE that the signature covers, so enrolling afterwards would break the
// signature.
func EnrollLimineConfigHash(efiPath, hashHex string) error {
	if len(hashHex) != limineHashHexLen {
		return fmt.Errorf("limine config hash must be %d hex chars, got %d", limineHashHexLen, len(hashHex))
	}
	if _, err := hex.DecodeString(hashHex); err != nil {
		return fmt.Errorf("limine config hash is not hex: %w", err)
	}

	buf, err := os.ReadFile(efiPath)
	if err != nil {
		return err
	}
	idx := bytes.Index(buf, []byte(limineB2SumMarker))
	if idx < 0 {
		return fmt.Errorf("%s: no %q marker — not a limine EFI binary, or too old to support Secure Boot config enrollment",
			filepath.Base(efiPath), limineB2SumMarker)
	}
	start := idx + len(limineB2SumMarker)
	if start+limineHashHexLen > len(buf) {
		return fmt.Errorf("%s: truncated before the config hash field", filepath.Base(efiPath))
	}
	// A second marker would mean we cannot tell which copy limine reads;
	// upstream's own tool takes the first match, but a duplicate signals a
	// binary we do not understand, so refuse rather than enrol the wrong one.
	if bytes.Contains(buf[start+limineHashHexLen:], []byte(limineB2SumMarker)) {
		return fmt.Errorf("%s: multiple %q markers", filepath.Base(efiPath), limineB2SumMarker)
	}
	copy(buf[start:start+limineHashHexLen], hashHex)

	// Preserve the original mode; this is an EFI executable staged for
	// signing, not a fresh file.
	fi, err := os.Stat(efiPath)
	if err != nil {
		return err
	}
	return os.WriteFile(efiPath, buf, fi.Mode().Perm())
}

// LimineConfigEnrolled reports whether a limine EFI binary already carries a
// non-zero config hash, i.e. whether Secure Boot enforcement is switched on.
// Used by tests and by the build's own verification pass.
func LimineConfigEnrolled(efiPath string) (bool, error) {
	buf, err := os.ReadFile(efiPath)
	if err != nil {
		return false, err
	}
	idx := bytes.Index(buf, []byte(limineB2SumMarker))
	if idx < 0 {
		return false, fmt.Errorf("%s: no limine config-hash marker", filepath.Base(efiPath))
	}
	start := idx + len(limineB2SumMarker)
	if start+limineHashHexLen > len(buf) {
		return false, fmt.Errorf("%s: truncated before the config hash field", filepath.Base(efiPath))
	}
	field := buf[start : start+limineHashHexLen]
	return !bytes.Equal(field, bytes.Repeat([]byte("0"), limineHashHexLen)), nil
}

// BuildLimineSecureConfig renders a limine.conf whose kernel and initramfs
// paths are pinned by BLAKE2B hash.
//
// Every loadable path must carry a hash: with a config checksum enrolled,
// limine panics on any unhashed path rather than loading it. That is the
// property being bought here — the signature covers the config, and the config
// covers the kernel.
func BuildLimineSecureConfig(kernelPath, initrdPath, cmdline, rootLabel string) (string, error) {
	kernelHash, err := Blake2bFile(kernelPath)
	if err != nil {
		return "", fmt.Errorf("hashing kernel: %w", err)
	}

	base := "fslabel(" + rootLabel + "):/boot"
	var b strings.Builder
	b.WriteString("# Generated by osb — Secure Boot enrolled; do not edit.\n")
	b.WriteString("# Editing this file invalidates the enrolled BLAKE2B and limine will refuse to boot.\n")
	b.WriteString("timeout: 3\n")
	b.WriteString("serial: yes\n\n")
	b.WriteString("/osb\n")
	b.WriteString("    protocol: linux\n")
	fmt.Fprintf(&b, "    path: %s/%s#%s\n", base, filepath.Base(kernelPath), kernelHash)
	fmt.Fprintf(&b, "    cmdline: %s\n", cmdline)
	if initrdPath != "" {
		initrdHash, err := Blake2bFile(initrdPath)
		if err != nil {
			return "", fmt.Errorf("hashing initramfs: %w", err)
		}
		fmt.Fprintf(&b, "    module_path: %s/%s#%s\n", base, filepath.Base(initrdPath), initrdHash)
	}
	return b.String(), nil
}

// checkLimineSecureBootTools verifies the host can sign an EFI binary. sbsign
// is separate from the ukify path's tooling: ukify builds and signs a UKI in
// one step, whereas limine ships a finished EFI application that only needs a
// signature.
func checkLimineSecureBootTools() error {
	return checkSecureBootTools("sbsign", "mcopy")
}

// SignImageLimine installs a Secure-Boot-enforcing limine onto the ESP of a
// built disk image.
//
// Order matters and is not interchangeable:
//
//	hash kernel/initrd -> render config -> hash config -> enrol into EFI
//	  -> sign EFI -> copy both to the ESP
//
// Enrolling after signing would invalidate the signature; signing before
// enrolling would sign a binary that enforces nothing.
func SignImageLimine(diskPath, cmdline, rootLabel, arch string, keyPEM, certPEM []byte) error {
	if err := checkLimineSecureBootTools(); err != nil {
		return err
	}
	if arch != "x86_64" {
		return fmt.Errorf("Secure Boot with limine is x86_64-only in osb (arch %q)", arch)
	}

	rootfs := filepath.Join(filepath.Dir(diskPath), "rootfs")
	kernel, initrd := findBootKernel(diskPath)
	if kernel == "" {
		return fmt.Errorf("Secure Boot: no kernel in the built rootfs to pin in limine.conf")
	}
	// The limine unit installs the EFI application here; the machine must
	// carry it or there is nothing to sign.
	srcEFI := filepath.Join(rootfs, "usr", "share", "limine", "BOOTX64.EFI")
	if _, err := os.Stat(srcEFI); err != nil {
		return fmt.Errorf("Secure Boot: %s missing from the rootfs — add \"limine\" to the machine's packages: %w", srcEFI, err)
	}

	conf, err := BuildLimineSecureConfig(kernel, initrd, cmdline, rootLabel)
	if err != nil {
		return err
	}
	confHash, err := Blake2bBytes([]byte(conf))
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "osb-limine-sb-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	keyPath := filepath.Join(work, "db.key")
	crtPath := filepath.Join(work, "db.crt")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing key: %w", err)
	}
	if err := os.WriteFile(crtPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("writing cert: %w", err)
	}

	confPath := filepath.Join(work, "limine.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		return err
	}

	stagedEFI := filepath.Join(work, "BOOTX64.EFI")
	if err := copyFile(srcEFI, stagedEFI); err != nil {
		return fmt.Errorf("staging limine EFI binary: %w", err)
	}
	if err := EnrollLimineConfigHash(stagedEFI, confHash); err != nil {
		return err
	}

	signedEFI := filepath.Join(work, "BOOTX64.signed.efi")
	if out, err := exec.Command("sbsign",
		"--key", keyPath, "--cert", crtPath,
		"--output", signedEFI, stagedEFI).CombinedOutput(); err != nil {
		return fmt.Errorf("signing limine EFI binary: %w\n%s", err, out)
	}

	if err := installUKIToESP(diskPath, signedEFI, "/EFI/BOOT/"+EFIBootName(arch)); err != nil {
		return err
	}
	if err := installUKIToESP(diskPath, confPath, LimineESPConfigPath); err != nil {
		return fmt.Errorf("writing limine.conf to ESP: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
