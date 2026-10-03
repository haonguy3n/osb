package main

import (
	"flag"
	"os"

	"github.com/anhhao17/osb/internal/device"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	machine := fs.String("machine", "", "target machine (default: defaults.machine)")
	distro := fs.String("distro", "", "target distro (default: defaults.distro)")
	bootTest := fs.Bool("boot-test", false, "boot headless, log in over SSH, power off; non-zero exit on failure")
	iso := fs.Bool("iso", false, "boot the installer ISO against a blank disk")
	daemon := fs.Bool("daemon", false, "run QEMU in the background")
	memory := fs.String("memory", "", "guest RAM, e.g. 4G (default: the machine's)")
	diskSize := fs.String("disk-size", "8G", "grow a copy of the disk to this size for the run")
	positional := parse(fs, args)

	proj := loadProject(*machine, *distro)
	image := proj.Defaults.Image
	if len(positional) > 0 {
		image = positional[0]
	}
	fail(device.RunQEMU(proj, image, proj.Defaults.Machine, projectDir(), device.QEMUOptions{
		Memory:   *memory,
		Daemon:   *daemon,
		DiskSize: *diskSize,
		ISO:      *iso,
		BootTest: *bootTest,
	}, os.Stdout))
}
