package main

import (
	"flag"
	"fmt"
	"os"

	osb "github.com/anhhao17/osb/internal"
)

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	machine := fs.String("machine", "", "default machine (default: qemu-x86_64)")
	distro := fs.String("distro", "", "default distro: alpine, debian or ubuntu (default: alpine)")
	positional := parse(fs, args)
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: osb init <dir> [-distro name] [-machine name]")
		os.Exit(2)
	}
	fail(osb.RunInit(positional[0], *machine, *distro))
}
