package main

import (
	"flag"
	"fmt"
	"os"

	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/build"
)

func cmdClean(args []string) {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	all := fs.Bool("all", false, "remove all build artifacts")
	force := fs.Bool("force", false, "skip confirmation prompt")
	locks := fs.Bool("locks", false, "remove stale lock files")
	fs.BoolVar(force, "f", false, "skip confirmation prompt (shorthand)")
	fs.Parse(args)

	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}

	if *locks {
		if err := osb.CleanLocks(dir, build.Arch()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := osb.RunClean(dir, build.Arch(), *all, *force, fs.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
