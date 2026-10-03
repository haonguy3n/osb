package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/anhhao17/osb/internal/device"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	machineName := fs.String("machine", "", "target machine")
	distroName := fs.String("distro", "", "target distro for this run (overrides local.star/defaults; useful when an image name exists in multiple distros)")
	memory := fs.String("memory", "", "RAM size, e.g. 8G (overrides the machine's qemu memory; saved to local.star)")
	display := fs.Bool("display", false, "enable graphical display")
	daemon := fs.Bool("daemon", false, "run in background")
	bootTest := fs.Bool("boot-test", false, "boot headless, wait for the login prompt, SSH in and run a health check, then power off; exits non-zero on any failure")
	bootTimeout := fs.Duration("timeout", 0, "boot-test timeout (e.g. 90s, 5m); 0 uses the default")
	diskSize := fs.String("disk-size", "8G", "grow QEMU disk image to this size for the run (empty to disable)")
	iso := fs.Bool("iso", false, "boot the image's installer ISO with a blank target disk")
	var ports stringSlice
	fs.Var(&ports, "port", "host:guest port forwarding (repeatable); a matching guest port replaces the machine's default forward")
	fs.Parse(args)
	var positional []string
	for rest := fs.Args(); len(rest) > 0; rest = fs.Args() {
		positional = append(positional, rest[0])
		fs.Parse(rest[1:])
	}

	displaySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "display" {
			displaySet = true
		}
	})

	opts := device.QEMUOptions{
		Ports:           ports,
		Display:         *display,
		Daemon:          *daemon,
		DiskSize:        *diskSize,
		ISO:             *iso,
		BootTest:        *bootTest,
		BootTestTimeout: *bootTimeout,
	}

	if root, err := findProjectRootForLocal(projectDir()); err == nil {
		ov, _ := osbstar.LoadLocalOverrides(root)
		opts.Memory = ov.QEMUMemory
		if *memory != "" {
			opts.Memory = *memory
			if ov.QEMUMemory != *memory {
				ov.QEMUMemory = *memory
				if werr := osbstar.WriteLocalOverrides(root, ov); werr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not save qemu_memory to local.star: %v\n", werr)
				}
			}
		}
		if !displaySet {
			opts.Display = ov.QEMUDisplay == "on"
		}
		if len(ov.QEMUPorts) > 0 {
			opts.Ports = append(append([]string(nil), ov.QEMUPorts...), opts.Ports...)
		}
	} else if *memory != "" {
		opts.Memory = *memory
	}

	ensureStdlibFeeds(effectiveDistroHint(*distroName), archHint(*machineName))
	proj := loadProjectWithMachineDistro(*machineName, *distroName)
	unitName := ""
	if len(positional) > 0 {
		unitName = positional[0]
	}
	if unitName == "" {
		unitName = proj.Defaults.Image
	}
	if unitName == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s run <image-unit> [--machine <name>]\n", os.Args[0])
		os.Exit(1)
	}

	if err := device.RunQEMU(proj, unitName, *machineName, projectDir(), opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
