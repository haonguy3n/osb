package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

var version = "dev"

type command struct {
	name    string
	args    string
	summary string
	run     func(args []string)
}

var commands = []command{
	{"init", "<dir>", "create a project (-distro, -machine)", cmdInit},
	{"build", "[units]", "build the default image or the named units (-machine, -distro, -force, -all, -j)", cmdBuild},
	{"run", "[image]", "boot an image in QEMU (-machine, -distro, -boot-test, -iso, -daemon, -memory, -disk-size)", cmdRun},
	{"flash", "<image> <disk>", "write an image to a disk (-machine, -distro, -yes); flash list shows disks", cmdFlash},
	{"key", "[secure-boot]", "show the package signing key, or create a Secure Boot key", cmdKey},
	{"log", "[unit]", "print the latest build log, or one unit's", cmdLog},
	{"clean", "[units]", "remove build output (-all also removes the package repo)", cmdClean},
	{"shell", "", "open a shell in the build container", cmdShell},
	{"binfmt", "", "register qemu-user to build arm64 on x86_64", cmdBinfmt},
	{"version", "", "print the version", func([]string) { fmt.Println(version) }},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	for _, c := range commands {
		if c.name == os.Args[1] {
			c.run(os.Args[2:])
			return
		}
	}
	if a := os.Args[1]; a != "help" && a != "-h" && a != "--help" {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", a)
	}
	usage()
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: osb <command> [flags]\n\n")
	width := 0
	for _, c := range commands {
		width = max(width, len(c.name)+1+len(c.args))
	}
	for _, c := range commands {
		fmt.Fprintf(os.Stderr, "  %-*s  %s\n", width, strings.TrimSpace(c.name+" "+c.args), c.summary)
	}
	fmt.Fprintf(os.Stderr, "\nOSB_PROJECT sets the project directory (default: current directory).\n")
}

func parse(fs *flag.FlagSet, args []string) []string {
	var positional []string
	for {
		_ = fs.Parse(args)
		args = fs.Args()
		if len(args) == 0 {
			return positional
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
