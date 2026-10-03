package device

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestMergeQEMUPorts(t *testing.T) {
	machine := []string{"2222:22", "8080:80", "8118:8118"}

	tests := []struct {
		name    string
		machine []string
		cli     []string
		want    []string
	}{
		{
			name:    "no CLI ports keeps machine defaults",
			machine: machine,
			cli:     nil,
			want:    []string{"2222:22", "8080:80", "8118:8118"},
		},
		{
			name:    "matching guest port replaces the machine forward",
			machine: machine,
			cli:     []string{"18118:8118"},
			want:    []string{"2222:22", "8080:80", "18118:8118"},
		},
		{
			name:    "new guest port is appended",
			machine: machine,
			cli:     []string{"9000:9000"},
			want:    []string{"2222:22", "8080:80", "8118:8118", "9000:9000"},
		},
		{
			name:    "qemu-in-qemu: every default forward remapped",
			machine: machine,
			cli:     []string{"12222:22", "18080:80", "18118:8118"},
			want:    []string{"12222:22", "18080:80", "18118:8118"},
		},
		{
			name:    "replace and append mixed",
			machine: machine,
			cli:     []string{"18080:80", "9000:9000"},
			want:    []string{"2222:22", "18080:80", "8118:8118", "9000:9000"},
		},
		{
			name:    "malformed CLI entry is appended untouched",
			machine: machine,
			cli:     []string{"nonsense"},
			want:    []string{"2222:22", "8080:80", "8118:8118", "nonsense"},
		},
		{
			name:    "no machine ports, CLI only",
			machine: nil,
			cli:     []string{"2222:22"},
			want:    []string{"2222:22"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeQEMUPorts(tt.machine, tt.cli)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MergeQEMUPorts(%v, %v) = %v, want %v", tt.machine, tt.cli, got, tt.want)
			}
		})
	}
}

// TestMergeQEMUPortsDoesNotMutateMachine guards against the merge aliasing
// and writing through the machine's declared slice.
func TestMergeQEMUPortsDoesNotMutateMachine(t *testing.T) {
	machine := []string{"2222:22", "8118:8118"}
	_ = MergeQEMUPorts(machine, []string{"18118:8118"})
	if machine[1] != "8118:8118" {
		t.Errorf("machine slice was mutated: %v", machine)
	}
}

// TestCheckQEMUPortsAvailable_OverrideRetargetsBusyPort reproduces the Setup
// → QEMU settings fix end-to-end at the preflight layer: a machine forward on
// a host port that's already bound is moved off it by a local override, and
// the availability check must honor the override (test the remapped port)
// rather than the original machine port. Passing nil overrides - the old TUI
// behavior - must still flag the collision.
func TestCheckQEMUPortsAvailable_OverrideRetargetsBusyPort(t *testing.T) {
	// Bind a port to stand in for "8080 is already taken".
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind busy port: %v", err)
	}
	defer busy.Close()
	busyPort := strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)

	// Grab a second ephemeral port, then release it so it's free to remap onto.
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind free port: %v", err)
	}
	freePort := strconv.Itoa(freeLn.Addr().(*net.TCPAddr).Port)
	freeLn.Close()

	machine := &osbstar.Machine{
		QEMU: &osbstar.QEMUConfig{Ports: []string{busyPort + ":8080"}},
	}

	// No override: the machine forward still points at the busy port → error.
	if err := CheckQEMUPortsAvailable(machine, nil); err == nil {
		t.Fatalf("expected a collision on busy port %s with no override", busyPort)
	}

	// Override remaps guest 8080 onto the free host port → must pass.
	if err := CheckQEMUPortsAvailable(machine, []string{freePort + ":8080"}); err != nil {
		t.Fatalf("override %s:8080 should clear the collision, got: %v", freePort, err)
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

func TestQEMUArgsDisplay(t *testing.T) {
	p := testPlan("x86_64", nil)
	if a := p.args(); !slices.Contains(a, "-nographic") {
		t.Errorf("headless run should use -nographic: %v", a)
	}
	p.opts.Display = true
	a := p.args()
	if slices.Contains(a, "-nographic") || !slices.Contains(a, "mon:stdio") {
		t.Errorf("display run should open a window and keep serial on stdio: %v", a)
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
