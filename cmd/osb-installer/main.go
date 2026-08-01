// Command osb-installer installs a running osb live image onto a disk.
//
// It runs on the target machine, from the live medium, and is the interactive
// counterpart to `osb flash` (which writes a prebuilt image from a build host).
// The guided flow collects a target disk, an optional LUKS2 passphrase, a
// hostname and accounts, shows exactly what will be destroyed, and only then
// touches the disk.
//
// The prompt flow is deliberately line-based rather than a full-screen TUI:
// the installer's primary console on the machines osb targets is a serial
// port, where cursor addressing is unreliable and a scrollback of what
// happened is more useful than a redrawn screen.
//
// Non-interactive use for fleet provisioning:
//
//	osb-installer -config install.conf      # answers from a file, no prompts
//	osb-installer -config install.conf -dry-run
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/anhhao17/osb/internal/installer"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nosb-installer: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath = flag.String("config", "", "answer file for an unattended install (no prompts)")
		dryRun     = flag.Bool("dry-run", false, "print the install plan without changing anything")
		yes        = flag.Bool("yes", false, "skip the final confirmation (implied by -config)")
	)
	flag.Parse()

	// Ctrl-C between prompts should abort cleanly. Once partitioning has
	// started there is nothing to roll back to, but the context still stops
	// the sequence at the next step boundary rather than mid-command.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var req installer.Request
	var err error
	if *configPath != "" {
		req, err = loadConfig(*configPath)
	} else {
		if os.Geteuid() != 0 && !*dryRun {
			return errors.New("must run as root (or pass -dry-run)")
		}
		req, err = prompt(ctx)
	}
	if err != nil {
		return err
	}

	steps, err := installer.Plan(req)
	if err != nil {
		return err
	}

	if *dryRun {
		fmt.Printf("\nInstall plan (%d steps) — nothing will be changed:\n\n", len(steps))
		return installer.Execute(ctx, steps, installer.DryRunner{Out: os.Stdout}, nil)
	}

	if !*yes && *configPath == "" {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("  ALL DATA ON %s WILL BE DESTROYED.\n", req.Disk)
		fmt.Printf("%s\n\n", strings.Repeat("=", 60))
		ok, err := confirm("Type 'yes' to proceed")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted")
		}
	}

	log, err := os.Create("/tmp/osb-installer.log")
	if err != nil {
		return fmt.Errorf("opening log: %w", err)
	}
	defer log.Close()

	fmt.Println()
	err = installer.Execute(ctx, steps, installer.ExecRunner{Log: log}, func(n, total int, s installer.Step) {
		fmt.Printf("[%2d/%2d] %s\n", n, total, s.Desc)
	})
	if err != nil {
		// The target is now half-installed; say so rather than let the user
		// assume the disk is untouched.
		return fmt.Errorf("%w\n\nThe target disk is in a partially installed state.\n"+
			"See /tmp/osb-installer.log, then re-run the installer to start over.", err)
	}

	fmt.Printf("\nInstallation complete. Remove the installation medium and reboot.\n")
	return nil
}

// --- interactive flow ------------------------------------------------

var stdin = bufio.NewReader(os.Stdin)

func prompt(ctx context.Context) (installer.Request, error) {
	var r installer.Request

	fw := installer.FirmwareOfHost()
	fmt.Printf("osb installer\n")
	fmt.Printf("Firmware detected: %s\n\n", strings.ToUpper(string(fw)))
	r.Firmware = fw
	r.SourceRoot = "/"

	disk, err := chooseDisk()
	if err != nil {
		return r, err
	}
	r.Disk = disk

	// Encryption is UEFI-only: a BIOS layout has no unencrypted partition to
	// stage the kernel and bootloader on. Don't offer what Validate refuses.
	if fw == installer.FirmwareUEFI {
		enc, err := confirm("Encrypt the root filesystem with LUKS2?")
		if err != nil {
			return r, err
		}
		r.Encrypt = enc
		if enc {
			pass, err := readPassphrase("Encryption passphrase")
			if err != nil {
				return r, err
			}
			r.Passphrase = pass
		}

		sb, err := confirm("Boot via signed UKI (Secure Boot)?")
		if err != nil {
			return r, err
		}
		r.SecureBoot = sb
	} else {
		fmt.Println("Note: disk encryption and Secure Boot need UEFI; skipping both.")
	}

	if r.Hostname, err = readLine("Hostname", "osb"); err != nil {
		return r, err
	}
	if r.Username, err = readLine("Username (blank for none)", ""); err != nil {
		return r, err
	}
	if r.Username != "" {
		if r.UserPassword, err = readPassphrase("Password for " + r.Username); err != nil {
			return r, err
		}
	}
	if r.RootPassword, err = readPassphrase("Root password (blank to lock root)"); err != nil {
		return r, err
	}

	// Validate before the confirmation screen so a typo is caught while it is
	// still cheap to fix.
	if err := r.Validate(); err != nil {
		return r, err
	}
	summarize(r)
	return r, nil
}

func chooseDisk() (string, error) {
	disks, err := installer.ListDisks()
	if err != nil {
		return "", err
	}
	if len(disks) == 0 {
		return "", errors.New("no installable disks found")
	}
	fmt.Println("Available disks:")
	for i, d := range disks {
		fmt.Printf("  %d) %s\n", i+1, d)
	}
	for {
		s, err := readLine("Install to which disk?", "1")
		if err != nil {
			return "", err
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 1 || n > len(disks) {
			fmt.Printf("  Enter a number between 1 and %d.\n", len(disks))
			continue
		}
		d := disks[n-1]
		if d.Removable {
			fmt.Printf("  %s is removable — it may be the medium you booted from.\n", d.Path)
			ok, err := confirm("  Install onto it anyway?")
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
		}
		return d.Path, nil
	}
}

func summarize(r installer.Request) {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Printf("\nSummary\n-------\n")
	fmt.Printf("  Disk:        %s (all data destroyed)\n", r.Disk)
	fmt.Printf("  Firmware:    %s\n", strings.ToUpper(string(r.Firmware)))
	fmt.Printf("  Encrypted:   %s\n", yn(r.Encrypt))
	fmt.Printf("  Secure Boot: %s\n", yn(r.SecureBoot))
	fmt.Printf("  Hostname:    %s\n", r.Hostname)
	if r.Username != "" {
		fmt.Printf("  User:        %s\n", r.Username)
	} else {
		fmt.Printf("  User:        (none)\n")
	}
	if r.RootPassword == "" {
		fmt.Printf("  Root:        locked\n")
	}
}

func readLine(label, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	s, err := stdin.ReadString('\n')
	if err != nil {
		return "", err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return s, nil
}

// readPassphrase reads without echoing when stdin is a terminal, and asks
// twice so a typo cannot silently become an unopenable LUKS container.
func readPassphrase(label string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Piped input (a test harness or an automated run): read one line.
		return readLine(label, "")
	}
	for {
		fmt.Printf("%s: ", label)
		b1, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		if len(b1) == 0 {
			return "", nil
		}
		fmt.Printf("%s (again): ", label)
		b2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		if string(b1) == string(b2) {
			return string(b1), nil
		}
		fmt.Println("  Passphrases did not match; try again.")
	}
}

func confirm(label string) (bool, error) {
	s, err := readLine(label+" [y/N]", "n")
	if err != nil {
		return false, err
	}
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes", nil
}

// --- unattended flow -------------------------------------------------

// loadConfig reads a key=value answer file. Unknown keys are an error rather
// than a silent no-op: a typo'd "encrypt" in a fleet-provisioning file would
// otherwise ship unencrypted machines.
func loadConfig(path string) (installer.Request, error) {
	r := installer.Request{SourceRoot: "/", Firmware: installer.FirmwareOfHost()}
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return r, fmt.Errorf("%s:%d: not key=value: %q", path, i+1, line)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "disk":
			r.Disk = v
		case "firmware":
			r.Firmware = installer.Firmware(v)
		case "encrypt":
			r.Encrypt = isTrue(v)
		case "passphrase":
			r.Passphrase = v
		case "secure_boot":
			r.SecureBoot = isTrue(v)
		case "hostname":
			r.Hostname = v
		case "username":
			r.Username = v
		case "user_password":
			r.UserPassword = v
		case "root_password":
			r.RootPassword = v
		case "source_root":
			r.SourceRoot = v
		default:
			return r, fmt.Errorf("%s:%d: unknown key %q", path, i+1, k)
		}
	}
	return r, r.Validate()
}

func isTrue(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}
