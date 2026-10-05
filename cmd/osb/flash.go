package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/anhhao17/osb/internal/device"
)

func cmdFlash(args []string) {
	if len(args) > 0 && args[0] == "list" {
		cands, err := device.ListCandidates()
		fail(err)
		if len(cands) == 0 {
			fmt.Println("No removable disks found.")
			return
		}
		fmt.Printf("%-14s %8s  %-4s %-10s %s\n", "DEVICE", "SIZE", "BUS", "VENDOR", "MODEL")
		for _, c := range cands {
			fmt.Printf("%-14s %8s  %-4s %-10s %s\n", c.Path, device.FormatSize(c.Size), c.Bus, c.Vendor, c.Model)
		}
		return
	}
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	machine := fs.String("machine", "", "machine the image was built for (default: defaults.machine)")
	distro := fs.String("distro", "", "distro the image was built for (default: defaults.distro)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	positional := parse(fs, args)
	if len(positional) != 2 {
		fmt.Fprintln(os.Stderr, "usage: osb flash <image> <disk> | osb flash list")
		os.Exit(2)
	}
	proj := loadProject(*machine, *distro)
	image := positional[0]
	if u := proj.AnyUnit(image); u != nil && u.Boot.Has("secureboot") {
		if _, _, isTest := device.SecureBootKeyMaterial(projectDir()); isTest {
			fmt.Fprintln(os.Stderr, "WARNING: this image is signed with osb's public test key; run `osb key secure-boot` and rebuild before shipping it.")
		}
	}
	fail(device.Flash(proj, image, positional[1], projectDir(), *yes, os.Stdout))
}
