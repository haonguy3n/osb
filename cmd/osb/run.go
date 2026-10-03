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
	// 8G default gives grow-rootfs ~6 GiB of slack past the 2 GiB
	// partition to expand into - enough to exercise the grow path and
	// hold the Docker image cache during on-target work. Pass an empty
	// string to disable and run against disk.img directly.
	diskSize := fs.String("disk-size", "8G", "grow QEMU disk image to this size for the run (empty to disable)")
	iso := fs.Bool("iso", false, "boot the image's installer ISO with a blank target disk")
	var ports stringSlice
	fs.Var(&ports, "port", "host:guest port forwarding (repeatable); a matching guest port replaces the machine's default forward")
	// Go's flag package stops parsing at the first non-flag argument, so
	// `osb run base-image --port ...` would silently drop every flag after
	// the image name. Re-parse the tail after each positional so flags and
	// the image name may appear in any order.
	fs.Parse(args)
	var positional []string
	for rest := fs.Args(); len(rest) > 0; rest = fs.Args() {
		positional = append(positional, rest[0])
		fs.Parse(rest[1:])
	}

	// Whether --display was set on the command line (vs. left at its default
	// false). Distinguishes "user asked for no display" from "user didn't
	// say" so the local.star tri-state can take over in the latter case.
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

	// QEMU memory precedence: --memory flag > local.star qemu_memory >
	// the machine's own qemu memory (resolved in device.RunQEMU when
	// opts.Memory is empty). A --memory value is persisted so subsequent
	// runs (and the TUI) reuse it without re-passing the flag.
	//
	// QEMU display precedence: --display flag > local.star qemu_display
	// > false. Only --display on the command line writes to local.star;
	// the TUI is the editor for the persisted preference.
	//
	// QEMU ports: local.star qemu_ports are appended to the CLI --port
	// list before the run-side merge with the machine's declared forwards.
	// The CLI list comes last so a one-off --port still beats a saved
	// override for the same guest port.
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

	// Apply the distro override the same way `osb build --distro` does, so a
	// run targets the matching distro's built image when an image name (e.g.
	// dev-image) exists in more than one distro. Sits at the local.star
	// override level in the cascade. Threaded into the loader so image()
	// resolves its distro_artifacts branch against the requested distro
	// during evaluation rather than against a stale local.star override.
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
