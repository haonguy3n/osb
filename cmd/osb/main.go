package main

import (
	"fmt"
	"os"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

var version = "dev"

var (
	globalProjectFile            string
	globalShowShadows            bool
	globalAllowDuplicateProvides = true
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type command struct {
	name    string
	args    string
	summary string
	run     func(args []string)
}

var commands = []command{
	{"init", "<project-dir>", "Create a new project (-distro, -machine)", cmdInit},
	{"build", "[units...]", "Build the default image or named units (-machine, -distro, -force, -all)", cmdBuild},
	{"run", "", "Boot an image in QEMU (-boot-test, -iso, -daemon)", cmdRun},
	{"flash", "<unit> <device>", "Write an image to a device/SD card (also: flash list)", cmdFlash},
	{"key", "<generate|info|secure-boot>", "Manage signing keys (apk repo + Secure Boot)", cmdKey},
	{"shell", "", "Open a shell in the build container (debug a failing unit)", cmdShell},
	{"binfmt", "", "Register QEMU user-mode emulation, for building arm64 on x86_64", cmdBinfmt},
	{"log", "[unit] [-e]", "Show build log (most recent, or a specific unit)", cmdLog},
	{"clean", "", "Remove build artifacts", cmdClean},
	{"version", "", "Display version information", cmdVersion},
}

func lookup(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func cmdVersion([]string) { fmt.Println(version) }

func main() {
	args := os.Args[1:]
	for i := 0; i < len(args); {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			globalProjectFile = args[i+1]
			args = append(args[:i], args[i+2:]...)
		case args[i] == "--show-shadows":
			globalShowShadows = true
			args = append(args[:i], args[i+1:]...)
		case args[i] == "--allow-duplicate-provides":
			globalAllowDuplicateProvides = true
			args = append(args[:i], args[i+1:]...)
		default:
			i++
		}
	}

	if len(args) < 1 {
		printUsage()
		return
	}

	name := args[0]
	cmdArgs := args[1:]

	switch name {
	case "--help", "-h", "help":
		printUsage()
		return
	}

	if c := lookup(name); c != nil {
		c.run(cmdArgs)
		return
	}
	if !tryCustomCommand(name, cmdArgs) {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", name)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [GLOBAL OPTIONS] COMMAND [OPTIONS]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Osb embedded Linux distribution builder\n\n")
	fmt.Fprintf(os.Stderr, "Global Options:\n")
	fmt.Fprintf(os.Stderr, "  --project <file>            Use an alternative project file instead of PROJECT.star\n")
	fmt.Fprintf(os.Stderr, "  --show-shadows              Print stderr notices about cross-module unit shadowing\n")
	fmt.Fprintf(os.Stderr, "                              and intra-module provides overrides\n")
	fmt.Fprintf(os.Stderr, "  --allow-duplicate-provides  Allow multiple units in the same module to declare\n")
	fmt.Fprintf(os.Stderr, "                              the same virtual provide (first registered wins)\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	width := 0
	for _, c := range commands {
		if n := len(c.name) + 1 + len(c.args); n > width {
			width = n
		}
	}
	for _, c := range commands {
		fmt.Fprintf(os.Stderr, "  %-*s  %s\n", width, strings.TrimSpace(c.name+" "+c.args), c.summary)
	}
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s init -distro ubuntu my-project\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s build\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s run -boot-test\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s build -machine qemu-arm64 secure-image\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s flash my-image /dev/sdX\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Environment Variables:\n")
	fmt.Fprintf(os.Stderr, "  OSB_PROJECT             Project directory (default: cwd)\n")
	fmt.Fprintf(os.Stderr, "  OSB_CACHE               Cache directory (default: cache/ in project dir)\n")
	fmt.Fprintf(os.Stderr, "  OSB_LOG                 Log level: debug, info, warn, error (default: info)\n")
	fmt.Fprintf(os.Stderr, "\n")
}

func tryCustomCommand(command string, args []string) bool {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}

	cmds, engines, err := osbstar.LoadCommands(dir)
	if err != nil {
		return false
	}

	cmd, ok := cmds[command]
	if !ok {
		return false
	}

	eng := engines[command]
	if err := osbstar.RunCommand(eng, cmd, args, dir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return true
}
