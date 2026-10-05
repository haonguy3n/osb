package device

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

type QEMUOptions struct {
	Memory   string
	Daemon   bool
	DiskSize string
	ISO      bool
	BootTest bool
}

type qemuPlan struct {
	bin      string
	arch     string
	machine  *osbstar.Machine
	boot     *osbstar.Boot
	disk     string
	cdrom    string
	code     string
	vars     string
	tpmSock  string
	useKVM   bool
	opts     QEMUOptions
	firmware string
}

func checkQEMUPortsFree(ports []string) error {
	for _, p := range ports {
		host, _, ok := strings.Cut(p, ":")
		if !ok || host == "" {
			continue
		}
		ln, err := net.Listen("tcp", ":"+host)
		if err != nil {
			return fmt.Errorf("host port %s is already in use - a guest from an earlier `osb run` is probably still running", host)
		}
		_ = ln.Close()
	}
	return nil
}

func qemuStderrTail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return "\n" + line
		}
	}
	return ""
}

func RunQEMU(proj *osbstar.Project, unitName, machineName, projectDir string, opts QEMUOptions, w io.Writer) error {
	unit := proj.AnyUnit(unitName)
	if unit == nil {
		return fmt.Errorf("unit %q not found", unitName)
	}
	if unit.Class != "image" {
		return fmt.Errorf("unit %q is not an image", unitName)
	}
	if machineName == "" {
		machineName = proj.Defaults.Machine
	}
	machine, ok := proj.Machines[machineName]
	if !ok {
		return fmt.Errorf("machine %q not found", machineName)
	}
	distro, err := proj.EffectiveDistroForImage(unitName)
	if err != nil {
		return fmt.Errorf("resolving distro for %q: %w", unitName, err)
	}
	imgPath := findImage(projectDir, machine.Name, unitName, distro)
	if imgPath == "" {
		return fmt.Errorf("no built image for %q - run osb build %s first", unitName, unitName)
	}
	plan := &qemuPlan{
		bin:      qemuBinary(machine.Arch),
		arch:     machine.Arch,
		machine:  machine,
		boot:     unit.Boot,
		opts:     opts,
		firmware: machine.Firmware,
		useKVM:   machine.Arch == detectHostArch() && kvmAvailable(),
	}
	if _, err := exec.LookPath(plan.bin); err != nil {
		return fmt.Errorf("%s not found on PATH - install QEMU for %s (Debian/Ubuntu: qemu-system-x86 / qemu-system-arm)", plan.bin, machine.Arch)
	}
	if err := checkQEMUPortsFree(machine.QEMUPorts()); err != nil {
		return err
	}
	if !plan.useKVM && machine.Arch == detectHostArch() {
		fmt.Fprintln(w, "  /dev/kvm not available - using TCG software emulation (slower)")
	}

	if opts.ISO {
		iso := strings.TrimSuffix(imgPath, ".img") + ".iso"
		if _, err := os.Stat(iso); err != nil {
			return fmt.Errorf("no ISO for %q - set iso = True on the image and rebuild", unitName)
		}
		plan.cdrom = iso
		plan.disk = strings.TrimSuffix(imgPath, ".img") + ".target.img"
		size, err := parseSizeBytes(opts.DiskSize)
		if err != nil || opts.DiskSize == "" {
			size = 8 << 30
		}
		_ = os.Remove(plan.disk)
		f, err := os.Create(plan.disk)
		if err != nil {
			return err
		}
		if err := f.Truncate(size); err != nil {
			f.Close()
			return err
		}
		f.Close()
		fmt.Fprintf(w, "  Installer: booting %s with a blank %s target disk %s\n", filepath.Base(iso), FormatSize(size), filepath.Base(plan.disk))
	} else {
		plan.disk = imgPath
		if opts.DiskSize != "" {
			grown, err := ensureGrownQEMUImage(imgPath, opts.DiskSize)
			if err != nil {
				return fmt.Errorf("growing QEMU image: %w", err)
			}
			plan.disk = grown
		}
	}

	if plan.firmware == osbstar.FirmwareUEFI {
		if err := plan.prepareUEFI(imgPath, projectDir, w); err != nil {
			return err
		}
	}
	if plan.boot.Has("tpm") {
		stop, err := plan.startSWTPM(imgPath, w)
		if err != nil {
			return err
		}
		if !opts.Daemon {
			defer stop()
		}
	}

	args := plan.args()
	if opts.BootTest {
		sshPort, err := sshHostPort(machine)
		if err != nil {
			return err
		}
		return runBootTest(plan.bin, args, sshPort, w)
	}
	fmt.Fprintf(w, "Starting %s (%s)\n", plan.bin, machine.Name)
	cmd := exec.Command(plan.bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if opts.Daemon {
		cmd.Stdin, cmd.Stdout = nil, nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting QEMU: %w", err)
		}
		fmt.Fprintf(w, "QEMU running in background (PID %d)\n", cmd.Process.Pid)
		return nil
	}
	var errBuf strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &errBuf)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("QEMU exited with an error: %w%s", err, qemuStderrTail(errBuf.String()))
	}
	return nil
}

func (p *qemuPlan) prepareUEFI(imgPath, projectDir string, w io.Writer) error {
	secure := p.boot.Has("secureboot")
	code, template := uefiFirmware(p.arch, secure)
	if code == "" {
		pkg := "ovmf"
		if p.arch == "arm64" {
			pkg = "qemu-efi-aarch64"
		}
		return fmt.Errorf("no UEFI firmware (split CODE/VARS) for %s found on this host - install %s", p.arch, pkg)
	}
	p.code = code
	p.vars = strings.TrimSuffix(imgPath, ".img") + ".vars.fd"
	if fresh(p.vars, imgPath) && !p.opts.ISO {
		return nil
	}
	if !secure {
		return copySparse(template, p.vars)
	}
	if err := checkSecureBootRunTools(); err != nil {
		return err
	}
	_, certPEM, isTest := SecureBootKeyMaterial(projectDir)
	var bootFiles []string
	if p.boot != nil {
		for _, e := range p.boot.Entries {
			if e.Slot != "" {
				bootFiles = append(bootFiles, ABSlotUKIPath(e.Slot))
			}
		}
	}
	if err := EnrollSecureBootVars(p.vars, template, certPEM, bootFiles...); err != nil {
		return fmt.Errorf("preparing Secure Boot: %w", err)
	}
	src := "project key"
	if isTest {
		src = "embedded test key"
	}
	fmt.Fprintf(w, "  Secure Boot: enrolled %s into %s\n", src, filepath.Base(p.vars))
	return nil
}

func (p *qemuPlan) startSWTPM(imgPath string, w io.Writer) (func(), error) {
	if _, err := exec.LookPath("swtpm"); err != nil {
		return nil, fmt.Errorf("image uses the tpm feature but swtpm is not on PATH - install swtpm (Debian/Ubuntu: swtpm)")
	}
	dir := strings.TrimSuffix(imgPath, ".img") + ".tpm"
	if !fresh(dir, imgPath) || p.opts.ISO {
		_ = os.RemoveAll(dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p.tpmSock = filepath.Join(dir, "swtpm.sock")
	_ = os.Remove(p.tpmSock)
	cmd := exec.Command("swtpm", "socket", "--tpm2",
		"--tpmstate", "dir="+dir,
		"--ctrl", "type=unixio,path="+p.tpmSock,
		"--flags", "startup-clear",
		"--terminate")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting swtpm: %w", err)
	}
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(p.tpmSock); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(w, "  TPM: swtpm state in %s\n", filepath.Base(dir))
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}, nil
}

func fresh(path, than string) bool {
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(than)
	if err != nil {
		return true
	}
	return a.ModTime().After(b.ModTime())
}

func (p *qemuPlan) args() []string {
	m := p.machine
	q := m.QEMU
	if q == nil {
		q = &osbstar.QEMUConfig{}
	}
	secure := p.boot.Has("secureboot") && p.code != ""

	mtype := q.Machine
	if mtype == "" {
		mtype = "q35"
		if p.arch == "arm64" {
			mtype = "virt"
		}
	}
	if secure && p.arch == "x86_64" {
		mtype += ",smm=on"
	}
	a := []string{"-machine", mtype}
	if cpu := qemuCPU(q.CPU, p.arch, p.useKVM); cpu != "" {
		a = append(a, "-cpu", cpu)
	}
	if p.useKVM {
		a = append(a, "-enable-kvm")
	}
	mem := p.opts.Memory
	if mem == "" {
		mem = q.Memory
	}
	if mem == "" {
		mem = "2G"
	}
	a = append(a, "-m", mem, "-smp", "2")
	a = append(a, "-nographic")

	if p.code != "" {
		if secure && p.arch == "x86_64" {
			a = append(a, "-global", "driver=cfi.pflash01,property=secure,value=on")
		}
		a = append(a,
			"-drive", "if=pflash,unit=0,format=raw,readonly=on,file="+p.code,
			"-drive", "if=pflash,unit=1,format=raw,file="+p.vars)
	}

	a = append(a, "-device", "virtio-scsi-pci,id=scsi0")
	a = append(a, "-drive", "if=none,id=disk0,format=raw,file="+p.disk,
		"-device", "virtio-blk-pci,drive=disk0,bootindex=1")
	if p.cdrom != "" {
		a = append(a, "-drive", "if=none,id=cd0,media=cdrom,readonly=on,file="+p.cdrom,
			"-device", "scsi-cd,drive=cd0,bootindex=0")
	}

	netdev := "user,id=net0"
	for _, port := range m.QEMUPorts() {
		netdev += fmt.Sprintf(",hostfwd=tcp::%s", strings.Replace(port, ":", "-:", 1))
	}
	a = append(a, "-netdev", netdev, "-device", "virtio-net-pci,netdev=net0")

	if p.tpmSock != "" {
		dev := "tpm-tis"
		if p.arch == "arm64" {
			dev = "tpm-tis-device"
		}
		a = append(a,
			"-chardev", "socket,id=chrtpm,path="+p.tpmSock,
			"-tpmdev", "emulator,id=tpm0,chardev=chrtpm",
			"-device", dev+",tpmdev=tpm0")
	}
	return a
}

func uefiFirmware(arch string, secure bool) (code, vars string) {
	type pair struct{ code, vars string }
	var candidates []pair
	switch {
	case arch == "arm64" && secure:
		candidates = []pair{
			{"/usr/share/AAVMF/AAVMF_CODE.secboot.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
			{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
		}
	case arch == "arm64":
		candidates = []pair{
			{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
			{"/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw", "/usr/share/edk2/aarch64/vars-template-pflash.raw"},
			{"/usr/share/qemu/edk2-aarch64-code.fd", "/usr/share/qemu/edk2-arm-vars.fd"},
		}
	case secure:
		candidates = []pair{
			{"/usr/share/OVMF/OVMF_CODE_4M.secboot.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
			{"/usr/share/OVMF/OVMF_CODE.secboot.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
			{"/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
			{"/usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd", "/usr/share/edk2/x64/OVMF_VARS.4m.fd"},
		}
	default:
		candidates = []pair{
			{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
			{"/usr/share/OVMF/OVMF_CODE.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
			{"/usr/share/edk2/ovmf/OVMF_CODE.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
			{"/usr/share/edk2/x64/OVMF_CODE.4m.fd", "/usr/share/edk2/x64/OVMF_VARS.4m.fd"},
			{"/usr/share/qemu/edk2-x86_64-code.fd", "/usr/share/qemu/edk2-i386-vars.fd"},
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

func ensureGrownQEMUImage(src, targetSize string) (string, error) {
	targetBytes, err := parseSizeBytes(targetSize)
	if err != nil {
		return "", fmt.Errorf("parsing disk size %q: %w", targetSize, err)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if srcInfo.Size() >= targetBytes {
		return src, nil
	}
	dst := strings.TrimSuffix(src, ".img") + ".run.img"
	if fresh(dst, src) {
		if info, err := os.Stat(dst); err == nil && info.Size() >= targetBytes {
			return dst, nil
		}
	}
	_ = os.Remove(dst)
	if err := copySparse(src, dst); err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	if err := os.Truncate(dst, targetBytes); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

func copySparse(src, dst string) error {
	if err := exec.Command("cp", "--sparse=always", src, dst).Run(); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func parseSizeBytes(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult = 1 << 10
	case 'M', 'm':
		mult = 1 << 20
	case 'G', 'g':
		mult = 1 << 30
	case 'T', 't':
		mult = 1 << 40
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return n * mult, nil
}

func qemuBinary(arch string) string {
	if arch == "arm64" {
		return "qemu-system-aarch64"
	}
	return "qemu-system-x86_64"
}

func detectHostArch() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "x86_64"
	}
	if arch := strings.TrimSpace(string(out)); arch != "aarch64" {
		return arch
	}
	return "arm64"
}

func kvmAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func qemuCPU(configured, arch string, useKVM bool) string {
	if useKVM {
		return configured
	}
	if configured == "" || configured == "host" {
		return "max"
	}
	return configured
}
