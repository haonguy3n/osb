package starlark

import (
	"strings"
	"testing"
)

func TestEvalProject(t *testing.T) {
	src := `
project(
    name = "test-project",
    version = "0.1.0",
    defaults = defaults(machine = "qemu-arm64", image = "base-image"),
    cache = cache(path = "/var/cache/osb/build"),
)
`
	eng := NewEngine()
	if err := eng.ExecString("PROJECT.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	proj := eng.Project()
	if proj == nil {
		t.Fatal("Project() returned nil")
	}
	if proj.Name != "test-project" {
		t.Errorf("Name = %q, want %q", proj.Name, "test-project")
	}
	if proj.Defaults.Machine != "qemu-arm64" {
		t.Errorf("Defaults.Machine = %q, want %q", proj.Defaults.Machine, "qemu-arm64")
	}
	if proj.Defaults.Image != "base-image" {
		t.Errorf("Defaults.Image = %q, want %q", proj.Defaults.Image, "base-image")
	}
	if proj.Cache.Path != "/var/cache/osb/build" {
		t.Errorf("Cache.Path = %q, want %q", proj.Cache.Path, "/var/cache/osb/build")
	}
}

func TestEvalUnitDef(t *testing.T) {
	src := `
unit(
    name = "openssh",
    version = "9.6p1",
    source = "https://cdn.openbsd.org/pub/OpenBSD/OpenSSH/portable/openssh-9.6p1.tar.gz",
    sha256 = "abc123",
    deps = ["zlib", "openssl"],
    runtime_deps = ["zlib", "openssl"],
    tasks = [
        task("build", steps = [
            "./configure --prefix=$PREFIX",
            "make -j$NPROC",
            "make DESTDIR=$DESTDIR install",
        ]),
    ],
    services = ["sshd"],
    conffiles = ["/etc/ssh/sshd_config"],
)
`
	eng := NewEngine()
	if err := eng.ExecString("units/openssh.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	units := eng.Units()
	r, ok := units["openssh"]
	if !ok {
		t.Fatal("unit 'openssh' not found")
	}
	if r.Class != "unit" {
		t.Errorf("Class = %q, want %q", r.Class, "unit")
	}
	if r.Version != "9.6p1" {
		t.Errorf("Version = %q, want %q", r.Version, "9.6p1")
	}
	if len(r.Deps) != 2 {
		t.Errorf("Deps = %v, want 2 entries", r.Deps)
	}
	if len(r.Tasks) != 1 {
		t.Errorf("Tasks = %v, want 1 task", r.Tasks)
	} else if len(r.Tasks[0].Steps) != 3 {
		t.Errorf("Tasks[0].Steps = %v, want 3 steps", r.Tasks[0].Steps)
	}
	if len(r.Services) != 1 || r.Services[0] != "sshd" {
		t.Errorf("Services = %v, want [sshd]", r.Services)
	}
}

func TestEvalUnitWithTasks(t *testing.T) {
	src := `
unit(
    name = "zlib",
    version = "1.3.1",
    source = "https://zlib.net/zlib-1.3.1.tar.gz",
    tasks = [
        task("build", steps = [
            "./configure --prefix=$PREFIX",
            "make -j$NPROC",
            "make DESTDIR=$DESTDIR install",
        ]),
    ],
)
`
	eng := NewEngine()
	if err := eng.ExecString("units/zlib.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	r := eng.Units()["zlib"]
	if r.Class != "unit" {
		t.Errorf("Class = %q, want %q", r.Class, "unit")
	}
	if len(r.Tasks) != 1 {
		t.Fatalf("Tasks count = %d, want 1", len(r.Tasks))
	}
	if r.Tasks[0].Name != "build" {
		t.Errorf("Tasks[0].Name = %q, want %q", r.Tasks[0].Name, "build")
	}
	if len(r.Tasks[0].Steps) != 3 {
		t.Errorf("Tasks[0].Steps count = %d, want 3", len(r.Tasks[0].Steps))
	}
}

func TestEvalUnitWithTaskContainer(t *testing.T) {
	src := `
unit(
    name = "myapp",
    version = "1.2.3",
    source = "https://github.com/example/myapp.git",
    tag = "v1.2.3",
    tasks = [
        task("build", container = "golang:1.22",
            steps = ["go build -o $DESTDIR/usr/bin/myapp ./cmd/myapp"],
        ),
    ],
)
`
	eng := NewEngine()
	if err := eng.ExecString("units/myapp.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	r := eng.Units()["myapp"]
	if r.Class != "unit" {
		t.Errorf("Class = %q, want %q", r.Class, "unit")
	}
	if len(r.Tasks) != 1 {
		t.Fatalf("Tasks count = %d, want 1", len(r.Tasks))
	}
	if r.Tasks[0].Container != "golang:1.22" {
		t.Errorf("Tasks[0].Container = %q, want %q", r.Tasks[0].Container, "golang:1.22")
	}
	if len(r.Tasks[0].Steps) != 1 {
		t.Errorf("Tasks[0].Steps count = %d, want 1", len(r.Tasks[0].Steps))
	}
}

func TestEvalInvalidArch(t *testing.T) {
	src := `machine(name = "bad", arch = "mips")`
	eng := NewEngine()
	err := eng.ExecString("machines/bad.star", src)
	if err == nil {
		t.Fatal("expected error for invalid arch, got nil")
	}
}

func TestEvalUnitWithPatches(t *testing.T) {
	src := `
unit(
    name = "busybox",
    version = "1.36.1",
    source = "https://busybox.net/downloads/busybox-1.36.1.tar.bz2",
    patches = [
        "busybox/fix-ash-segfault.patch",
        "busybox/add-custom-applet.patch",
    ],
    tasks = [
        task("build", steps = ["make -j$NPROC", "make DESTDIR=$DESTDIR install"]),
    ],
)
`
	eng := NewEngine()
	if err := eng.ExecString("units/busybox.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	r := eng.Units()["busybox"]
	if len(r.Patches) != 2 {
		t.Errorf("Patches = %v, want 2 entries", r.Patches)
	}
	if r.Patches[0] != "busybox/fix-ash-segfault.patch" {
		t.Errorf("Patches[0] = %q, want fix-ash-segfault.patch", r.Patches[0])
	}
}

func TestEvalUnitNoTasks(t *testing.T) {
	// Units without tasks are valid - they may get tasks from a class in Starlark.
	src := `unit(name = "minimal", version = "1.0.0")`
	eng := NewEngine()
	if err := eng.ExecString("units/minimal.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	r := eng.Units()["minimal"]
	if len(r.Tasks) != 0 {
		t.Errorf("Tasks = %v, want empty", r.Tasks)
	}
}

func TestEvalProjectDuplicate(t *testing.T) {
	src := `
project(name = "first", version = "1.0.0")
project(name = "second", version = "2.0.0")
`
	eng := NewEngine()
	err := eng.ExecString("PROJECT.star", src)
	if err == nil {
		t.Fatal("expected error for duplicate project(), got nil")
	}
}

func TestEvalUnitCacheDirs(t *testing.T) {
	src := `
unit(
    name = "mygo",
    version = "1.0.0",
    cache_dirs = {"/go/cache": "go"},
)
`
	eng := NewEngine()
	if err := eng.ExecString("units/mygo.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	u := eng.Units()["mygo"]
	if u == nil {
		t.Fatal("unit 'mygo' not found")
	}
	if len(u.CacheDirs) != 1 {
		t.Fatalf("CacheDirs = %v, want 1 entry", u.CacheDirs)
	}
	if u.CacheDirs["/go/cache"] != "go" {
		t.Errorf("CacheDirs[/go/cache] = %q, want %q", u.CacheDirs["/go/cache"], "go")
	}
}

func TestEvalUnitDuplicate(t *testing.T) {
	src := `
unit(name = "foo", version = "1.0.0")
unit(name = "foo", version = "2.0.0")
`
	eng := NewEngine()
	err := eng.ExecString("units/foo.star", src)
	if err == nil {
		t.Fatal("expected error for duplicate unit name, got nil")
	}
	if !strings.Contains(err.Error(), "already defined") {
		t.Errorf("error = %q, want it to contain 'already defined'", err)
	}
}

func TestRegisterUnit_CapturesExtraKwargs(t *testing.T) {
	eng := NewEngine()
	src := `
unit(
    name = "my-app",
    version = "1.0.0",
    port = 8080,
    log_level = "info",
    enable_tls = True,
    workers = 4,
    tasks = [],
)
`
	if err := eng.ExecString("test.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	u := eng.Units()["my-app"]
	if u == nil {
		t.Fatal("unit not registered")
	}
	if u.Extra == nil {
		t.Fatal("Extra is nil")
	}
	if got := u.Extra["port"]; got != int64(8080) {
		t.Errorf("Extra[port] = %v (%T), want int64(8080)", got, got)
	}
	if got := u.Extra["log_level"]; got != "info" {
		t.Errorf("Extra[log_level] = %v, want \"info\"", got)
	}
	if got := u.Extra["enable_tls"]; got != true {
		t.Errorf("Extra[enable_tls] = %v, want true", got)
	}
	if got := u.Extra["workers"]; got != int64(4) {
		t.Errorf("Extra[workers] = %v (%T), want int64(4)", got, got)
	}
	// Known fields must NOT appear in Extra
	if _, ok := u.Extra["name"]; ok {
		t.Error("Extra[name] should not be set (name is a typed field)")
	}
	if _, ok := u.Extra["tasks"]; ok {
		t.Error("Extra[tasks] should not be set (tasks is a typed field)")
	}
}

func TestEvalMachine(t *testing.T) {
	src := `
machine(
    name = "board",
    arch = "arm64",
    description = "A board",
    console = "ttyAMA0",
    cmdline = "quiet",
    kernel = {"alpine": "linux-lts", "debian": "linux-image-arm64"},
    packages = ["firmware"],
    distro_packages = {"alpine": ["fw-alpine"]},
    qemu = qemu_config(machine = "virt", cpu = "max", memory = "1G", ports = ["2222:22"]),
)
`
	eng := NewEngine()
	if err := eng.ExecString("machines/board.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	m := eng.Machines()["board"]
	if m == nil {
		t.Fatal("machine not registered")
	}
	if m.Firmware != FirmwareUEFI {
		t.Errorf("Firmware = %q, want uefi by default", m.Firmware)
	}
	if m.KernelFor("debian") != "linux-image-arm64" || m.KernelFor("ubuntu") != "" {
		t.Errorf("KernelFor: %v", m.Kernel)
	}
	if m.Console != "ttyAMA0" || m.Cmdline != "quiet" {
		t.Errorf("console/cmdline = %q/%q", m.Console, m.Cmdline)
	}
	if m.QEMU == nil || m.QEMU.Machine != "virt" || len(m.QEMUPorts()) != 1 {
		t.Errorf("QEMU = %+v", m.QEMU)
	}
	if got := m.DistroPackages["alpine"]; len(got) != 1 || got[0] != "fw-alpine" {
		t.Errorf("DistroPackages = %v", m.DistroPackages)
	}
}

func TestEvalMachineKernelString(t *testing.T) {
	eng := NewEngine()
	if err := eng.ExecString("m.star", `machine(name = "m", arch = "x86_64", firmware = "bios", kernel = "linux-custom")`); err != nil {
		t.Fatal(err)
	}
	m := eng.Machines()["m"]
	if m.KernelFor("alpine") != "linux-custom" || m.KernelFor("ubuntu") != "linux-custom" {
		t.Errorf("a string kernel applies to every distro: %v", m.Kernel)
	}
	if m.Firmware != FirmwareBIOS {
		t.Errorf("Firmware = %q", m.Firmware)
	}
}

func TestEvalMachineRejects(t *testing.T) {
	for name, src := range map[string]string{
		"bad arch":     `machine(name = "m", arch = "mips")`,
		"bad firmware": `machine(name = "m", arch = "x86_64", firmware = "coreboot")`,
		"bios on arm":  `machine(name = "m", arch = "arm64", firmware = "bios")`,
	} {
		if err := NewEngine().ExecString("m.star", src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEvalImageBoot(t *testing.T) {
	src := `
image(
    name = "img",
    packages = ["openssh", "myapp"],
    hostname = "osb",
    boot = {
        "loader": "uki",
        "firmware": "uefi",
        "features": ["secureboot", "verity"],
        "entries": [
            {"slot": "a", "root": "root-a", "hash": "root-a-hash", "cmdline": "console=ttyS0", "initial": True},
            {"slot": "b", "root": "root-b", "hash": "", "cmdline": "console=ttyS0", "initial": False},
        ],
    },
)
`
	eng := NewEngine()
	if err := eng.ExecString("images/img.star", src); err != nil {
		t.Fatalf("ExecString: %v", err)
	}
	r := eng.Units()["img"]
	if r == nil || r.Class != "image" {
		t.Fatalf("image not registered: %+v", r)
	}
	if len(r.Packages) != 2 {
		t.Errorf("Packages = %v", r.Packages)
	}
	if r.Extra["hostname"] != "osb" {
		t.Errorf("hostname should land in Extra: %v", r.Extra)
	}
	b := r.Boot
	if b == nil || b.Loader != "uki" || !b.Has("verity") || b.Has("tpm") {
		t.Fatalf("Boot = %+v", b)
	}
	if len(b.Entries) != 2 || !b.Entries[0].Initial || b.Entries[0].Hash != "root-a-hash" || b.Entries[1].Root != "root-b" {
		t.Errorf("Entries = %+v", b.Entries)
	}
}
