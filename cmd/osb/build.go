package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/anhhao17/osb/internal/build"
)

func cmdBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	machine := fs.String("machine", "", "target machine (default: defaults.machine)")
	distro := fs.String("distro", "", "target distro (default: defaults.distro)")
	force := fs.Bool("force", false, "rebuild the named units even when cached")
	all := fs.Bool("all", false, "build every unit in the project")
	jobs := fs.Int("j", 0, "units to build in parallel (default 5)")
	units := parse(fs, args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	proj := loadProject(*machine, *distro)
	m := proj.Machines[proj.Defaults.Machine]
	if len(units) == 0 && !*all && proj.Defaults.Image != "" {
		units = []string{proj.Defaults.Image}
	}
	opts := build.Options{
		Ctx:        ctx,
		Force:      *force,
		Parallel:   *jobs,
		ProjectDir: projectDir(),
		Arch:       m.Arch,
		Machine:    m.Name,
	}
	for _, n := range units {
		if d, err := proj.EffectiveDistroForImage(n); err == nil {
			opts.EffectiveDistro = d
			break
		}
	}
	fail(build.BuildUnits(proj, units, opts, os.Stdout))
}
