package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/anhhao17/osb/internal/build"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func resolveTargetArch(proj *osbstar.Project, machineName string) (string, error) {
	if machineName != "" {
		m, ok := proj.Machines[machineName]
		if !ok {
			return "", fmt.Errorf("machine %q not found", machineName)
		}
		return m.Arch, nil
	}
	if m, ok := proj.Machines[proj.Defaults.Machine]; ok {
		return m.Arch, nil
	}
	return build.Arch(), nil
}

func cmdBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	force := fs.Bool("force", false, "force rebuild even if cached")
	clean := fs.Bool("clean", false, "clean build directory before building")
	noCache := fs.Bool("no-cache", false, "disable cache lookup")
	dryRun := fs.Bool("dry-run", false, "show what would be built without building")
	verbose := fs.Bool("verbose", false, "verbose output")
	machineName := fs.String("machine", "", "target machine")
	distroName := fs.String("distro", "", "target distro for this build (overrides local.star/defaults; useful when an image name exists in multiple distros)")
	all := fs.Bool("all", false, "build every unit in the project (default: the project's default image)")
	jobs := fs.Int("jobs", 0, "max units to build in parallel (saved to local.star; default 5)")
	fs.BoolVar(verbose, "v", false, "verbose output (shorthand)")
	fs.IntVar(jobs, "j", 0, "max units to build in parallel (shorthand)")
	fs.Parse(args)

	units := fs.Args()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ensureStdlibFeeds(effectiveDistroHint(*distroName), archHint(*machineName))

	proj := loadProjectWithMachineDistro(*machineName, *distroName)
	targetArch, err := resolveTargetArch(proj, *machineName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	resolvedMachine := *machineName
	if resolvedMachine == "" {
		resolvedMachine = proj.Defaults.Machine
	}
	pdir := projectDir()
	opts := build.Options{
		Ctx:        ctx,
		Force:      *force,
		Clean:      *clean,
		NoCache:    *noCache,
		DryRun:     *dryRun,
		Verbose:    *verbose,
		ProjectDir: pdir,
		Arch:       targetArch,
		Machine:    resolvedMachine,
	}

	if len(units) == 0 && !*all && proj.Defaults.Image != "" {
		units = []string{proj.Defaults.Image}
	}
	if len(units) >= 1 {
		for _, n := range units {
			if u := proj.LookupUnit(proj.DefaultDistro, n); u != nil && u.Class == "image" {
				if d, err := proj.EffectiveDistroForImage(n); err == nil {
					opts.EffectiveDistro = d
					break
				}
			}
			for name, u := range proj.AllUnits() {
				if name == n && u.Class == "image" {
					if d, err := proj.EffectiveDistroForImage(n); err == nil {
						opts.EffectiveDistro = d
					}
					break
				}
			}
			if opts.EffectiveDistro != "" {
				break
			}
		}
	}

	if root, err := findProjectRootForLocal(pdir); err == nil {
		ov, _ := osbstar.LoadLocalOverrides(root)
		opts.Parallel = ov.ParallelBuilds
		if *jobs > 0 {
			opts.Parallel = *jobs
			if ov.ParallelBuilds != *jobs {
				ov.ParallelBuilds = *jobs
				if werr := osbstar.WriteLocalOverrides(root, ov); werr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not save parallel_builds to local.star: %v\n", werr)
				}
			}
		}
	} else if *jobs > 0 {
		opts.Parallel = *jobs
	}

	if err := build.BuildUnits(proj, units, opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
