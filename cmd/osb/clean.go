package main

import (
	"flag"

	osb "github.com/anhhao17/osb/internal"
)

func cmdClean(args []string) {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	all := fs.Bool("all", false, "also remove the package repository")
	units := parse(fs, args)
	fail(osb.RunClean(projectDir(), *all, units))
}
