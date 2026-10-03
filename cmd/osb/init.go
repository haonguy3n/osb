package main

import (
	"flag"
	"fmt"
	"os"

	osb "github.com/anhhao17/osb/internal"
)

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	machine := fs.String("machine", "", "default machine for the project")
	distro := fs.String("distro", "", "default distro for the project (alpine, debian, ubuntu)")
	// Go's flag package stops at the first non-flag argument, so
	// `osb init myproj --machine X` would drop the flag. Re-parse the tail
	// after each positional so the project dir and flags may appear in any
	// order.
	fs.Parse(args)
	var positional []string
	for rest := fs.Args(); len(rest) > 0; rest = fs.Args() {
		positional = append(positional, rest[0])
		fs.Parse(rest[1:])
	}

	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s init <project-dir> [-machine <name>] [-distro <name>]\n", os.Args[0])
		os.Exit(1)
	}

	if err := osb.RunInit(positional[0], *machine, *distro); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
