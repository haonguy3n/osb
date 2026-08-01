package device

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLimineEFI builds a stand-in for a limine EFI binary: some padding, the
// marker, 128 '0' characters, more padding. That is the exact shape
// enroll-config looks for, so patching logic can be tested without shipping a
// 350 KB binary in the repo.
func fakeLimineEFI(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(strings.Repeat("\x00PE-ish padding", 40))
	b.WriteString(limineB2SumMarker)
	b.Write(bytes.Repeat([]byte("0"), limineHashHexLen))
	b.WriteString(strings.Repeat("trailing\x00", 40))

	p := filepath.Join(t.TempDir(), "BOOTX64.EFI")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnrollLimineConfigHash(t *testing.T) {
	p := fakeLimineEFI(t)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	if enrolled, err := LimineConfigEnrolled(p); err != nil || enrolled {
		t.Fatalf("fresh binary reported enrolled=%v err=%v, want false/nil", enrolled, err)
	}

	if err := EnrollLimineConfigHash(p, testHash); err != nil {
		t.Fatalf("EnrollLimineConfigHash: %v", err)
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Only the 128-byte hash field may change: everything else is part of a
	// PE whose signature is computed over these bytes.
	if len(after) != len(before) {
		t.Fatalf("binary changed size: %d -> %d", len(before), len(after))
	}
	idx := bytes.Index(after, []byte(limineB2SumMarker))
	start := idx + len(limineB2SumMarker)
	if got := string(after[start : start+limineHashHexLen]); got != testHash {
		t.Errorf("enrolled hash = %q, want %q", got, testHash)
	}
	// Bytes outside the field are untouched.
	if !bytes.Equal(before[:start], after[:start]) {
		t.Error("bytes before the hash field changed")
	}
	tail := start + limineHashHexLen
	if !bytes.Equal(before[tail:], after[tail:]) {
		t.Error("bytes after the hash field changed")
	}

	if enrolled, err := LimineConfigEnrolled(p); err != nil || !enrolled {
		t.Errorf("after enrolling, enrolled=%v err=%v, want true/nil", enrolled, err)
	}
}

func TestEnrollLimineConfigHashRejects(t *testing.T) {
	t.Run("short hash", func(t *testing.T) {
		if err := EnrollLimineConfigHash(fakeLimineEFI(t), "abc"); err == nil {
			t.Error("accepted a short hash")
		}
	})
	t.Run("non-hex hash", func(t *testing.T) {
		bad := strings.Repeat("z", limineHashHexLen)
		if err := EnrollLimineConfigHash(fakeLimineEFI(t), bad); err == nil {
			t.Error("accepted a non-hex hash")
		}
	})
	t.Run("no marker", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "not-limine.efi")
		if err := os.WriteFile(p, []byte(strings.Repeat("x", 4096)), 0o644); err != nil {
			t.Fatal(err)
		}
		err := EnrollLimineConfigHash(p, testHash)
		if err == nil {
			t.Fatal("accepted a binary with no marker")
		}
		if !strings.Contains(err.Error(), "not a limine EFI binary") {
			t.Errorf("unhelpful error: %v", err)
		}
	})
}

// TestBlake2bMatchesB2sum pins osb's digest against the reference
// implementation. A silently different hash here produces images that panic at
// boot with "CHECKSUM MISMATCH", which is expensive to debug on hardware.
func TestBlake2bMatchesB2sum(t *testing.T) {
	b2sum, err := exec.LookPath("b2sum")
	if err != nil {
		t.Skip("b2sum not installed")
	}
	p := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(p, []byte("osb limine secure boot\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(b2sum, p).Output()
	if err != nil {
		t.Fatalf("b2sum: %v", err)
	}
	want := strings.Fields(string(out))[0]

	got, err := Blake2bFile(p)
	if err != nil {
		t.Fatalf("Blake2bFile: %v", err)
	}
	if got != want {
		t.Errorf("Blake2bFile = %s\nb2sum      = %s", got, want)
	}
	// b2sum defaults to BLAKE2b-512, which is what limine's
	// BLAKE2B_OUT_BYTES (64) requires.
	if len(got) != limineHashHexLen {
		t.Errorf("digest is %d hex chars, limine requires %d", len(got), limineHashHexLen)
	}
}

// TestBlake2bBytesMatchesFile: the config is hashed in memory before being
// written, so the two entry points must agree.
func TestBlake2bBytesMatchesFile(t *testing.T) {
	content := []byte("timeout: 3\nserial: yes\n")
	p := filepath.Join(t.TempDir(), "limine.conf")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := Blake2bFile(p)
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, err := Blake2bBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	if fromFile != fromBytes {
		t.Errorf("Blake2bFile = %s, Blake2bBytes = %s", fromFile, fromBytes)
	}
}

// TestBuildLimineSecureConfigPinsEveryPath is the security-critical assertion:
// with a config checksum enrolled, limine panics on any loadable path that has
// no hash. An unhashed path here would not fail the build — it would fail at
// boot, or worse, silently load an unverified file if enforcement were off.
func TestBuildLimineSecureConfigPinsEveryPath(t *testing.T) {
	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinuz")
	initrd := filepath.Join(dir, "initramfs")
	if err := os.WriteFile(kernel, []byte("fake kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initrd, []byte("fake initramfs"), 0o644); err != nil {
		t.Fatal(err)
	}

	conf, err := BuildLimineSecureConfig(kernel, initrd, "console=ttyS0 root=LABEL=rootfs rw", "rootfs")
	if err != nil {
		t.Fatalf("BuildLimineSecureConfig: %v", err)
	}

	kh, _ := Blake2bFile(kernel)
	ih, _ := Blake2bFile(initrd)
	wantKernel := "path: fslabel(rootfs):/boot/vmlinuz#" + kh
	wantInitrd := "module_path: fslabel(rootfs):/boot/initramfs#" + ih
	if !strings.Contains(conf, wantKernel) {
		t.Errorf("kernel path not pinned by hash:\n%s", conf)
	}
	if !strings.Contains(conf, wantInitrd) {
		t.Errorf("initramfs path not pinned by hash:\n%s", conf)
	}

	// Every path:/module_path: line must carry a '#'.
	for _, line := range strings.Split(conf, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "path:") || strings.HasPrefix(l, "module_path:") {
			if !strings.Contains(l, "#") {
				t.Errorf("unhashed loadable path: %q", l)
			}
		}
	}

	// A changed kernel must change the config, or the pin means nothing.
	if err := os.WriteFile(kernel, []byte("tampered kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf2, err := BuildLimineSecureConfig(kernel, initrd, "console=ttyS0 root=LABEL=rootfs rw", "rootfs")
	if err != nil {
		t.Fatal(err)
	}
	if conf == conf2 {
		t.Error("config unchanged after the kernel changed — the hash is not covering the file")
	}
}

// TestBuildLimineSecureConfigNoInitrd: Ubuntu images boot with built-in
// drivers and ship no initramfs; emitting an empty module_path would panic.
func TestBuildLimineSecureConfigNoInitrd(t *testing.T) {
	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinuz")
	if err := os.WriteFile(kernel, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err := BuildLimineSecureConfig(kernel, "", "root=LABEL=rootfs rw", "rootfs")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conf, "module_path") {
		t.Errorf("emitted module_path with no initramfs:\n%s", conf)
	}
}

// TestSecureBootToolHintsComplete: every tool named in a preflight check must
// have a package hint, or the user gets "install " with nothing after it —
// which is exactly what happened when sbsign was added without one.
func TestSecureBootToolHintsComplete(t *testing.T) {
	for _, tool := range []string{"ukify", "mcopy", "virt-fw-vars", "sbsign"} {
		if hint := secureBootToolHint[tool]; hint == "" {
			t.Errorf("no package hint for %q", tool)
		}
	}
}

// TestCheckSecureBootToolsMissingHint: even an unhinted tool must produce a
// sentence that ends cleanly rather than trailing off.
func TestCheckSecureBootToolsMissingHint(t *testing.T) {
	err := checkSecureBootTools("osb-definitely-not-a-real-binary")
	if err == nil {
		t.Fatal("expected an error for a missing tool")
	}
	if strings.HasSuffix(err.Error(), "install ") {
		t.Errorf("truncated hint: %v", err)
	}
}
