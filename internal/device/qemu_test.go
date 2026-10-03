package device

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestCheckQEMUPortsFree(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)
	if err := checkQEMUPortsFree([]string{busyPort + ":8080"}); err == nil {
		t.Fatalf("expected a collision on busy port %s", busyPort)
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := strconv.Itoa(free.Addr().(*net.TCPAddr).Port)
	free.Close()
	if err := checkQEMUPortsFree([]string{freePort + ":8080"}); err != nil {
		t.Fatalf("free port %s reported busy: %v", freePort, err)
	}
}

func testPlan(arch string, boot *osbstar.Boot) *qemuPlan {
	return &qemuPlan{
		bin:     qemuBinary(arch),
		arch:    arch,
		machine: &osbstar.Machine{Name: "m", Arch: arch, QEMU: &osbstar.QEMUConfig{Ports: []string{"2222:22"}}},
		boot:    boot,
		disk:    "/img/disk.img",
	}
}

func TestQEMUArgsSecureBootAndTPM(t *testing.T) {
	p := testPlan("x86_64", &osbstar.Boot{Features: []string{"secureboot", "tpm"}})
	p.code, p.vars, p.tpmSock = "/fw/CODE.fd", "/img/disk.vars.fd", "/img/disk.tpm/swtpm.sock"
	a := strings.Join(p.args(), " ")
	for _, want := range []string{
		"-machine q35,smm=on",
		"driver=cfi.pflash01,property=secure,value=on",
		"if=pflash,unit=0,format=raw,readonly=on,file=/fw/CODE.fd",
		"if=pflash,unit=1,format=raw,file=/img/disk.vars.fd",
		"-tpmdev emulator,id=tpm0,chardev=chrtpm",
		"-device tpm-tis,tpmdev=tpm0",
		"hostfwd=tcp::2222-:22",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("args missing %q:\n%s", want, a)
		}
	}
}

func TestQEMUArgsArm64(t *testing.T) {
	p := testPlan("arm64", &osbstar.Boot{Features: []string{"tpm"}})
	p.tpmSock = "/s"
	a := strings.Join(p.args(), " ")
	if !strings.Contains(a, "-machine virt") || !strings.Contains(a, "tpm-tis-device") {
		t.Errorf("arm64 args: %s", a)
	}
	if strings.Contains(a, "smm=on") {
		t.Errorf("arm64 must not request SMM: %s", a)
	}
}

func TestQEMUArgsInstallerISO(t *testing.T) {
	p := testPlan("x86_64", nil)
	p.cdrom = "/img/disk.iso"
	a := strings.Join(p.args(), " ")
	if !strings.Contains(a, "media=cdrom,readonly=on,file=/img/disk.iso") || !strings.Contains(a, "scsi-cd,drive=cd0,bootindex=0") {
		t.Errorf("installer args: %s", a)
	}
}

func TestParseSizeBytes(t *testing.T) {
	for in, want := range map[string]int64{"512": 512, "4K": 4096, "8G": 8 << 30, "1T": 1 << 40} {
		got, err := parseSizeBytes(in)
		if err != nil || got != want {
			t.Errorf("parseSizeBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestEnsureGrownQEMUImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x.img")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureGrownQEMUImage(src, "1M")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "x.run.img") {
		t.Fatalf("grown path = %s", got)
	}
	if info, _ := os.Stat(got); info.Size() != 1<<20 {
		t.Fatalf("grown size = %d", info.Size())
	}
	if same, _ := ensureGrownQEMUImage(src, "2"); same != src {
		t.Fatalf("an image already large enough should be used in place, got %s", same)
	}
}
