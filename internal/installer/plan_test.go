package installer

import (
	"context"
	"strings"
	"testing"
)

// baseRequest is a valid UEFI, unencrypted install. Tests mutate a copy.
func baseRequest() Request {
	return Request{
		Disk:         "/dev/sda",
		Firmware:     FirmwareUEFI,
		Hostname:     "osb-box",
		Username:     "hao",
		UserPassword: "pw",
		RootPassword: "rootpw",
		SourceRoot:   "/",
	}
}

// findStep returns the first step whose Argv starts with the given command,
// or a zero Step.
func findStep(steps []Step, cmd string) (Step, bool) {
	for _, s := range steps {
		if len(s.Argv) > 0 && s.Argv[0] == cmd {
			return s, true
		}
	}
	return Step{}, false
}

func findWrite(steps []Step, path string) (Step, bool) {
	for _, s := range steps {
		if s.WritePath == path {
			return s, true
		}
	}
	return Step{}, false
}

// stepIndex returns the position of the first step matching pred, or -1.
func stepIndex(steps []Step, pred func(Step) bool) int {
	for i, s := range steps {
		if pred(s) {
			return i
		}
	}
	return -1
}

func TestPartitionDevice(t *testing.T) {
	cases := []struct {
		disk string
		n    int
		want string
	}{
		{"/dev/sda", 1, "/dev/sda1"},
		{"/dev/sdb", 2, "/dev/sdb2"},
		{"/dev/vda", 1, "/dev/vda1"},
		// NVMe and mmcblk need the p separator; without it the installer
		// formats a device that does not exist, or a different one.
		{"/dev/nvme0n1", 1, "/dev/nvme0n1p1"},
		{"/dev/nvme0n1", 2, "/dev/nvme0n1p2"},
		{"/dev/mmcblk0", 1, "/dev/mmcblk0p1"},
		{"/dev/loop0", 3, "/dev/loop0p3"},
	}
	for _, c := range cases {
		if got := PartitionDevice(c.disk, c.n); got != c.want {
			t.Errorf("PartitionDevice(%q, %d) = %q, want %q", c.disk, c.n, got, c.want)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Request)
		want error
	}{
		{"no disk", func(r *Request) { r.Disk = "" }, ErrNoDisk},
		{"no source", func(r *Request) { r.SourceRoot = "" }, ErrNoSource},
		{"no hostname", func(r *Request) { r.Hostname = "" }, ErrNoHostname},
		{"encrypt without passphrase", func(r *Request) { r.Encrypt = true }, ErrNoPassphi},
		{"bad hostname", func(r *Request) { r.Hostname = "-nope" }, ErrBadHostname},
		{"hostname with dot", func(r *Request) { r.Hostname = "a.b" }, ErrBadHostname},
		{"bad username", func(r *Request) { r.Username = "1bad" }, ErrBadUsername},
		{"username with slash", func(r *Request) { r.Username = "a/b" }, ErrBadUsername},
		{
			"no way to log in",
			func(r *Request) { r.RootPassword = ""; r.Username = "" },
			ErrNoPassword,
		},
		{
			"encryption on BIOS",
			func(r *Request) { r.Firmware = FirmwareBIOS; r.Encrypt = true; r.Passphrase = "x" },
			ErrBIOSEncrypt,
		},
		{
			"secure boot on BIOS",
			func(r *Request) { r.Firmware = FirmwareBIOS; r.SecureBoot = true },
			ErrBIOSSecureBoot,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := baseRequest()
			c.mut(&r)
			err := r.Validate()
			if err != c.want {
				t.Fatalf("Validate() = %v, want %v", err, c.want)
			}
			// Plan must refuse too - nothing may touch a disk before the
			// request is known good.
			if _, perr := Plan(r); perr == nil {
				t.Error("Plan() accepted an invalid request")
			}
		})
	}
}

// TestPlanUEFIUnencrypted pins the exact command sequence for the common case.
func TestPlanUEFIUnencrypted(t *testing.T) {
	steps, err := Plan(baseRequest())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	sfdisk, ok := findStep(steps, "sfdisk")
	if !ok {
		t.Fatal("no sfdisk step")
	}
	if got := strings.Join(sfdisk.Argv, " "); got != "sfdisk --label gpt /dev/sda" {
		t.Errorf("sfdisk argv = %q", got)
	}
	if !strings.Contains(sfdisk.Stdin, "type=uefi") || !strings.Contains(sfdisk.Stdin, "name=rootfs") {
		t.Errorf("sfdisk layout missing ESP or root:\n%s", sfdisk.Stdin)
	}

	// The ESP is partition 1 and root partition 2.
	if _, ok := findStep(steps, "mkfs.vfat"); !ok {
		t.Error("no ESP mkfs step")
	}
	mkfs, _ := findStep(steps, "mkfs.ext4")
	if got := strings.Join(mkfs.Argv, " "); got != "mkfs.ext4 -F -L rootfs /dev/sda2" {
		t.Errorf("root mkfs argv = %q, want it to format /dev/sda2", got)
	}

	// No encryption anywhere.
	if _, ok := findStep(steps, "cryptsetup"); ok {
		t.Error("unencrypted install planned a cryptsetup step")
	}

	// fstab addresses root by label, not by the device node the installer
	// happened to see.
	fstab, ok := findWrite(steps, "/mnt/target/etc/fstab")
	if !ok {
		t.Fatal("no fstab written")
	}
	if !strings.Contains(fstab.Content, "LABEL=rootfs\t/\text4") {
		t.Errorf("fstab root entry wrong:\n%s", fstab.Content)
	}
	if strings.Contains(fstab.Content, "/dev/sda") {
		t.Errorf("fstab hard-codes a device node:\n%s", fstab.Content)
	}
}

// TestPlanOrdering guards the invariants that make the sequence safe:
// partition before format, format before mount, copy before configure,
// and unmount last.
func TestPlanOrdering(t *testing.T) {
	steps, err := Plan(baseRequest())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	isCmd := func(c string) func(Step) bool {
		return func(s Step) bool { return len(s.Argv) > 0 && s.Argv[0] == c }
	}
	part := stepIndex(steps, isCmd("sfdisk"))
	mkfs := stepIndex(steps, isCmd("mkfs.ext4"))
	mount := stepIndex(steps, isCmd("mount"))
	copy := stepIndex(steps, isCmd("rsync"))
	fstab := stepIndex(steps, func(s Step) bool { return s.WritePath == "/mnt/target/etc/fstab" })
	umount := stepIndex(steps, isCmd("umount"))
	sync := stepIndex(steps, isCmd("sync"))

	for _, c := range []struct {
		name string
		a, b int
	}{
		{"partition before mkfs", part, mkfs},
		{"mkfs before mount", mkfs, mount},
		{"mount before copy", mount, copy},
		{"copy before fstab", copy, fstab},
		{"fstab before umount", fstab, umount},
		{"umount before sync", umount, sync},
	} {
		if c.a < 0 || c.b < 0 {
			t.Fatalf("%s: missing step (a=%d b=%d)", c.name, c.a, c.b)
		}
		if c.a >= c.b {
			t.Errorf("%s: index %d should precede %d", c.name, c.a, c.b)
		}
	}
	// sync is the very last thing to happen.
	if sync != len(steps)-1 {
		t.Errorf("sync at %d, want last (%d)", sync, len(steps)-1)
	}
}

// TestPlanEncrypted covers the LUKS path, including that the passphrase is
// only ever passed on stdin.
func TestPlanEncrypted(t *testing.T) {
	r := baseRequest()
	r.Encrypt = true
	r.Passphrase = "correct horse battery staple"
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	var sawFormat, sawOpen bool
	for _, s := range steps {
		if len(s.Argv) == 0 {
			continue
		}
		// The passphrase must never appear in a command line: argv is
		// world-readable via /proc.
		for _, a := range s.Argv {
			if strings.Contains(a, r.Passphrase) {
				t.Fatalf("passphrase leaked into argv: %v", s.Argv)
			}
		}
		if s.Argv[0] == "cryptsetup" && len(s.Argv) > 1 {
			switch s.Argv[1] {
			case "luksFormat":
				sawFormat = true
				if s.Stdin != r.Passphrase {
					t.Error("luksFormat did not receive the passphrase on stdin")
				}
				if !contains(s.Argv, "--type") || !contains(s.Argv, "luks2") {
					t.Errorf("luksFormat is not LUKS2: %v", s.Argv)
				}
			case "open":
				sawOpen = true
			}
		}
	}
	if !sawFormat || !sawOpen {
		t.Fatalf("missing luksFormat (%v) or open (%v)", sawFormat, sawOpen)
	}

	// The filesystem goes on the mapper node, not the raw partition.
	mkfs, _ := findStep(steps, "mkfs.ext4")
	if got := mkfs.Argv[len(mkfs.Argv)-1]; got != "/dev/mapper/cryptroot" {
		t.Errorf("mkfs target = %q, want the mapper node", got)
	}

	// crypttab exists and is not world-readable.
	ct, ok := findWrite(steps, "/mnt/target/etc/crypttab")
	if !ok {
		t.Fatal("no crypttab written")
	}
	if ct.Mode != 0o600 {
		t.Errorf("crypttab mode = %#o, want 0600", ct.Mode)
	}
	if !strings.Contains(ct.Content, "PARTLABEL=rootfs") {
		t.Errorf("crypttab should address the container by PARTLABEL:\n%s", ct.Content)
	}

	// The initramfs must gain cryptsetup or the root is unreachable.
	mk, ok := findWrite(steps, "/mnt/target/etc/mkinitfs/mkinitfs.conf")
	if !ok {
		t.Fatal("encrypted install did not configure mkinitfs")
	}
	if !strings.Contains(mk.Content, "cryptsetup") {
		t.Errorf("mkinitfs features lack cryptsetup: %s", mk.Content)
	}

	// The LUKS mapping is closed again at the end.
	last := steps[len(steps)-1]
	if last.Argv[0] != "sync" {
		t.Errorf("last step = %v, want sync", last.Argv)
	}
	if stepIndex(steps, func(s Step) bool {
		return len(s.Argv) > 1 && s.Argv[0] == "cryptsetup" && s.Argv[1] == "close"
	}) < 0 {
		t.Error("encrypted install never closes the LUKS container")
	}
}

// TestPlanEncryptedBootFromESP: limine cannot read LUKS, so an encrypted
// install must stage the kernel on the ESP and point the config at boot():.
func TestPlanEncryptedBootFromESP(t *testing.T) {
	r := baseRequest()
	r.Encrypt = true
	r.Passphrase = "x"
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	conf, ok := findWrite(steps, "/mnt/target/boot/efi/limine.conf")
	if !ok {
		t.Fatal("no limine.conf on the ESP")
	}
	if !strings.Contains(conf.Content, "path: boot():/vmlinuz") {
		t.Errorf("kernel path should be on the ESP via boot():\n%s", conf.Content)
	}
	if strings.Contains(conf.Content, "fslabel(rootfs)") {
		t.Errorf("encrypted install points limine at the encrypted root:\n%s", conf.Content)
	}
	// The cmdline must tell the initramfs what to unlock.
	if !strings.Contains(conf.Content, "cryptroot=PARTLABEL=rootfs") ||
		!strings.Contains(conf.Content, "cryptdm=cryptroot") ||
		!strings.Contains(conf.Content, "root=/dev/mapper/cryptroot") {
		t.Errorf("cmdline cannot unlock the root:\n%s", conf.Content)
	}
}

// TestPlanBIOS: MBR layout, single partition, limine into the MBR.
func TestPlanBIOS(t *testing.T) {
	r := baseRequest()
	r.Firmware = FirmwareBIOS
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	sfdisk, _ := findStep(steps, "sfdisk")
	if got := strings.Join(sfdisk.Argv, " "); got != "sfdisk --label dos /dev/sda" {
		t.Errorf("sfdisk argv = %q, want an MBR label", got)
	}
	if _, ok := findStep(steps, "mkfs.vfat"); ok {
		t.Error("BIOS install created an ESP")
	}
	mkfs, _ := findStep(steps, "mkfs.ext4")
	if got := mkfs.Argv[len(mkfs.Argv)-1]; got != "/dev/sda1" {
		t.Errorf("root device = %q, want /dev/sda1 (single partition)", got)
	}
	lim, ok := findStep(steps, "limine")
	if !ok {
		t.Fatal("no limine bios-install step")
	}
	if got := strings.Join(lim.Argv, " "); got != "limine bios-install /dev/sda" {
		t.Errorf("limine argv = %q", got)
	}
	// Config and stage 2 land on the root filesystem for BIOS.
	if _, ok := findWrite(steps, "/mnt/target/boot/limine/limine.conf"); !ok {
		t.Error("no limine.conf on the root filesystem")
	}
}

// TestPlanSecureBootUsesUKI: a Secure Boot install copies the signed EFI
// binary and writes no bootloader config, since the cmdline is inside the
// signature.
func TestPlanSecureBootUsesUKI(t *testing.T) {
	r := baseRequest()
	r.SecureBoot = true
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, ok := findWrite(steps, "/mnt/target/boot/efi/limine.conf"); ok {
		t.Error("Secure Boot install wrote a limine.conf; the UKI carries its own cmdline")
	}
	if _, ok := findStep(steps, "limine"); ok {
		t.Error("Secure Boot install ran limine")
	}
	idx := stepIndex(steps, func(s Step) bool {
		return len(s.Argv) > 0 && s.Argv[0] == "cp" &&
			strings.HasSuffix(s.Argv[len(s.Argv)-1], "/EFI/BOOT/BOOTX64.EFI")
	})
	if idx < 0 {
		t.Error("Secure Boot install never installed the signed UKI")
	}
}

// TestPlanLocksRootWithoutPassword: an install with a user but no root
// password must lock root rather than leave it passwordless.
func TestPlanLocksRootWithoutPassword(t *testing.T) {
	r := baseRequest()
	r.RootPassword = ""
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	idx := stepIndex(steps, func(s Step) bool {
		return len(s.Argv) > 3 && s.Argv[0] == "chroot" && s.Argv[2] == "passwd" && s.Argv[3] == "-l"
	})
	if idx < 0 {
		t.Error("root was left without a password and without being locked")
	}
}

// TestPasswordsNeverInArgv is the counterpart to the passphrase check: user
// and root passwords go to chpasswd on stdin.
func TestPasswordsNeverInArgv(t *testing.T) {
	r := baseRequest()
	r.RootPassword = "r00tsecret"
	r.UserPassword = "usersecret"
	steps, err := Plan(r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, s := range steps {
		for _, a := range s.Argv {
			if strings.Contains(a, "secret") {
				t.Fatalf("password leaked into argv: %v", s.Argv)
			}
		}
	}
}

// recordingRunner captures the steps Execute drives.
type recordingRunner struct{ seen []Step }

func (r *recordingRunner) Run(_ context.Context, s Step) error {
	r.seen = append(r.seen, s)
	return nil
}

func TestExecuteRunsEveryStepInOrder(t *testing.T) {
	steps, err := Plan(baseRequest())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	rr := &recordingRunner{}
	var progress int
	if err := Execute(context.Background(), steps, rr, func(n, total int, _ Step) {
		progress++
		if total != len(steps) {
			t.Errorf("progress total = %d, want %d", total, len(steps))
		}
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rr.seen) != len(steps) {
		t.Fatalf("ran %d steps, want %d", len(rr.seen), len(steps))
	}
	if progress != len(steps) {
		t.Errorf("progress called %d times, want %d", progress, len(steps))
	}
	for i := range steps {
		if steps[i].Desc != rr.seen[i].Desc {
			t.Fatalf("step %d out of order: got %q want %q", i, rr.seen[i].Desc, steps[i].Desc)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
