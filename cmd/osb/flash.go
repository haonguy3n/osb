package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/anhhao17/osb/internal/device"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

// warnTestKeyOnHardware prints a prominent warning before flashing a Secure Boot
// image that was signed with the embedded public test key. That key is public in
// git, so the image is not actually secure on real hardware; the fix is to run
// `osb key secure-boot` and rebuild.
func warnTestKeyOnHardware(proj *osbstar.Project, machineName string) {
	if machineName == "" {
		machineName = proj.Defaults.Machine
	}
	m, ok := proj.Machines[machineName]
	if !ok || !m.IsSecureBoot() {
		return
	}
	if _, _, isTest := device.SecureBootKeyMaterial(projectDir()); !isTest {
		return
	}
	fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: this Secure Boot image is signed with osb's PUBLIC TEST key.\n")
	fmt.Fprintf(os.Stderr, "    It is not secure on real hardware - anyone can forge a bootloader for it.\n")
	fmt.Fprintf(os.Stderr, "    Run `osb key secure-boot` to create a project key, then rebuild the image.\n\n")
}

func cmdFlash(args []string) {
	if len(args) > 0 && args[0] == "list" {
		cmdFlashList(args[1:])
		return
	}

	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	machineName := fs.String("machine", "", "target machine")
	dryRun := fs.Bool("dry-run", false, "show what would be flashed without writing")
	assumeYes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s flash <image-unit> <device> [--machine <name>] [--yes] [--dry-run]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s flash list\n", os.Args[0])
		os.Exit(1)
	}

	unitName := fs.Arg(0)
	devicePath := fs.Arg(1)

	if devicePath == "" && !*dryRun {
		fmt.Fprintf(os.Stderr, "Usage: %s flash <image-unit> <device>\n", os.Args[0])
		os.Exit(1)
	}

	proj := loadProjectWithMachine(*machineName)
	warnTestKeyOnHardware(proj, *machineName)
	if err := device.Flash(proj, unitName, devicePath, projectDir(), *dryRun, *assumeYes, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdFlashList(_ []string) {
	cands, err := device.ListCandidates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(cands) == 0 {
		fmt.Println("No removable devices detected.")
		return
	}
	fmt.Printf("%-14s %8s  %-4s %-10s %s\n", "DEVICE", "SIZE", "BUS", "VENDOR", "MODEL")
	for _, c := range cands {
		fmt.Printf("%-14s %8s  %-4s %-10s %s\n",
			c.Path, device.FormatSize(c.Size), c.Bus, c.Vendor, c.Model)
	}
}
