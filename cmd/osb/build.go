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
	// Use the default machine's arch
	if m, ok := proj.Machines[proj.Defaults.Machine]; ok {
		return m.Arch, nil
	}
	// Fallback to host arch
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
	all := fs.Bool("all", false, "build all units")
	jobs := fs.Int("jobs", 0, "max units to build in parallel (saved to local.star; default 5)")
	fs.BoolVar(verbose, "v", false, "verbose output (shorthand)")
	fs.IntVar(jobs, "j", 0, "max units to build in parallel (shorthand)")
	fs.Parse(args)

	units := fs.Args()
	if *all {
		units = nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// --distro is a per-invocation distro override. It sits exactly where
	// local.star's default_distro_override does in the cascade
	// (image.distro -> override -> defaults.distro), so for a same-named
	// image across distros it selects which variant builds - without
	// editing local.star. An image's own explicit distro still wins.
	//
	// Threaded into the loader (not patched onto proj afterward) because
	// image() resolves its distro_artifacts branch and packaging/disk
	// functions eagerly during Starlark evaluation; a post-load override
	// would leave that closure baked against the wrong distro.
	// Prepare the bundled feed indexes the load below will evaluate. They are
	// stripped from the embedded stdlib and fetched on demand, so a cold cache
	// pulls a fresh index (never a stale embedded snapshot) before evaluation.
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

	// Derive the consuming distro from the requested target. When the
	// user names an image, use that image's effective distro so the
	// per-distro view picks the right variants for cross-distro
	// same-name collisions. When the user names a non-image unit (or
	// no name - build everything), fall back to the project default.
	if len(units) >= 1 {
		for _, n := range units {
			if u := proj.LookupUnit(proj.DefaultDistro, n); u != nil && u.Class == "image" {
				if d, err := proj.EffectiveDistroForImage(n); err == nil {
					opts.EffectiveDistro = d
					break
				}
			}
			// Fall back: scan AllUnits for any module's variant
			// to catch images registered under a non-default distro.
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

	// Parallelism precedence: -j flag > local.star parallel_builds >
	// build.DefaultParallel. A -j value is also persisted so subsequent
	// builds (and the TUI) reuse it without re-passing the flag.
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
