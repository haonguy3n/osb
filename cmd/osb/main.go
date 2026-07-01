package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	embedded "github.com/anhhao17/osb"
	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/artifact"
	"github.com/anhhao17/osb/internal/build"
	"github.com/anhhao17/osb/internal/device"
	"github.com/anhhao17/osb/internal/feeds/alpine"
	"github.com/anhhao17/osb/internal/feeds/apt"
	"github.com/anhhao17/osb/internal/module"
	"github.com/anhhao17/osb/internal/repo"
	"github.com/anhhao17/osb/internal/resolve"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"github.com/anhhao17/osb/internal/stdlib"
)

var version = "dev"

var (
	globalProjectFile string
	globalShowShadows bool
	// Default true while units-alpine's linux-firmware-* fan-out (~100
	// packages all providing `linux-firmware-any`) keeps tripping the
	// strict intra-module collision check. Flip back once that's fixed
	// upstream.
	globalAllowDuplicateProvides = true
)

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	// Parse global flags before command dispatch
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

	command := args[0]
	cmdArgs := args[1:]

	switch command {
	case "--help", "-h", "help":
		printUsage()
		return
	case "version":
		fmt.Println(version)
	case "init":
		cmdInit(cmdArgs)
	case "container":
		cmdContainer(cmdArgs)
	case "module":
		cmdModule(cmdArgs)
	case "update-feeds":
		cmdUpdateFeeds(cmdArgs)
	case "build":
		cmdBuild(cmdArgs)
	case "flash":
		cmdFlash(cmdArgs)
	case "run":
		cmdRun(cmdArgs)
	case "config":
		cmdConfig(cmdArgs)
	case "repo":
		cmdRepo(cmdArgs)
	case "desc":
		cmdDesc(cmdArgs)
	case "refs":
		cmdRefs(cmdArgs)
	case "graph":
		cmdGraph(cmdArgs)
	case "log":
		cmdLog(cmdArgs)
	case "clean":
		cmdClean(cmdArgs)
	default:
		if !tryCustomCommand(command, cmdArgs) {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
			printUsage()
			os.Exit(1)
		}
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
	fmt.Fprintf(os.Stderr, "  init <project-dir>      Create a new Osb project\n")
	fmt.Fprintf(os.Stderr, "  container               Manage the build container (build, shell, status)\n")
	fmt.Fprintf(os.Stderr, "  build [units...]        Build units (--force, --clean, --verbose, --dry-run)\n")
	fmt.Fprintf(os.Stderr, "  flash <unit> <device>   Write an image to a device/SD card (also: flash list)\n")
	fmt.Fprintf(os.Stderr, "  run                     Run an image in QEMU\n")
	fmt.Fprintf(os.Stderr, "  module                  Manage external modules (fetch, sync, list)\n")
	fmt.Fprintf(os.Stderr, "  update-feeds            Refresh APKINDEX files for the alpine_feed declarations\n")
	fmt.Fprintf(os.Stderr, "                          in the current module (run inside a module repo)\n")
	fmt.Fprintf(os.Stderr, "  repo                    Manage the local apk package repository\n")
	fmt.Fprintf(os.Stderr, "  config                  View and edit project configuration\n")
	fmt.Fprintf(os.Stderr, "  desc <unit>             Describe a unit or target\n")
	fmt.Fprintf(os.Stderr, "  refs <unit>             Show reverse dependencies\n")
	fmt.Fprintf(os.Stderr, "  graph                   Visualize the dependency DAG\n")
	fmt.Fprintf(os.Stderr, "  log [unit] [-e]         Show build log (most recent, or specific unit; -e to edit)\n")
	fmt.Fprintf(os.Stderr, "  clean                   Remove build artifacts\n")
	fmt.Fprintf(os.Stderr, "  key <generate|info>     Manage the project's apk signing key\n")
	fmt.Fprintf(os.Stderr, "  version                 Display version information\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  %s init my-project --machine qemu-x86_64\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s build openssh\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s build base-image --machine x86_64\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Environment Variables:\n")
	fmt.Fprintf(os.Stderr, "  OSB_PROJECT             Project directory (default: cwd)\n")
	fmt.Fprintf(os.Stderr, "  OSB_CACHE               Cache directory (default: cache/ in project dir)\n")
	fmt.Fprintf(os.Stderr, "  OSB_LOG                 Log level: debug, info, warn, error (default: info)\n")
	fmt.Fprintf(os.Stderr, "\n")
}

// cmdUpdateFeeds is the entry point for the `osb update-feeds`
// subcommand. Runs inside a module repo, peeks MODULE.star for
// alpine_feed() and apt_feed() calls, then runs the matching
// updater(s) in sequence. A module declaring both runs both.
// Verifies each feed's signature against its declared keys/keyring;
// writes only — the maintainer reviews `git diff` and commits
// manually.
func cmdUpdateFeeds(args []string) {
	fs := flag.NewFlagSet("update-feeds", flag.ExitOnError)
	var (
		archCSV        = fs.String("arch", "", "comma-separated arches to fetch (default: every arch with an existing on-disk feed dir, falling back to all supported)")
		moduleDir      = fs.String("module-dir", "", "module directory holding MODULE.star (default: cwd)")
		allowKeyUpdate = fs.String("allow-key-update", "", "append a fingerprint to keys/allowed-fingerprints before verifying (Debian only)")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s update-feeds [--arch x86_64,arm64] [--module-dir DIR] [--allow-key-update FPR]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Fetch upstream index files for every alpine_feed()/apt_feed()\n")
		fmt.Fprintf(os.Stderr, "declared in the current module's MODULE.star. Verifies each feed's\n")
		fmt.Fprintf(os.Stderr, "signature against the in-tree trust list. Writes only; review the\n")
		fmt.Fprintf(os.Stderr, "diff and commit manually.\n")
	}
	_ = fs.Parse(args)

	dir := *moduleDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "update-feeds: %v\n", err)
			os.Exit(1)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "MODULE.star")); err != nil {
		fmt.Fprintf(os.Stderr, "update-feeds: %s: no MODULE.star here (run inside the module repo)\n", dir)
		os.Exit(1)
	}

	var arches []string
	if *archCSV != "" {
		for _, a := range strings.Split(*archCSV, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				arches = append(arches, a)
			}
		}
	}

	alpineDecls, alpineErr := alpine.PeekFeedDecls(dir)
	aptDecls, aptErr := apt.PeekFeedDecls(dir)
	if alpineErr != nil && aptErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", alpineErr)
		os.Exit(1)
	}

	ran := false
	if len(alpineDecls) > 0 {
		ran = true
		opts := alpine.UpdateOptions{ModuleDir: dir, Out: os.Stdout, Arches: arches}
		if err := alpine.UpdateFeeds(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	if len(aptDecls) > 0 {
		ran = true
		opts := apt.UpdateOptions{
			ModuleDir:      dir,
			Out:            os.Stdout,
			Arches:         arches,
			AllowKeyUpdate: *allowKeyUpdate,
		}
		if err := apt.UpdateFeeds(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	if !ran {
		fmt.Fprintf(os.Stderr, "update-feeds: no alpine_feed() or apt_feed() in %s/MODULE.star\n", dir)
		os.Exit(1)
	}
}

func cmdModule(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s module <sync|list|info> [...]\n", os.Args[0])
		os.Exit(1)
	}

	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}

	switch args[0] {
	case "sync":
		// Read only PROJECT.star, not module contents — so a broken module
		// can still be re-synced to pull in the fix that unblocks it.
		modules, err := osbstar.ProjectModuleRefs(dir, projectLoadOpts()...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if _, err := module.Sync(modules, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := osb.ListModules(dir, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "info":
		fmt.Fprintf(os.Stderr, "module info: not yet implemented\n")
		os.Exit(1)
	case "check-updates":
		fmt.Fprintf(os.Stderr, "module check-updates: not yet implemented\n")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown module subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

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

	_ = all // build all when no positional args — handled by empty units slice
	units := fs.Args()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// --distro is a per-invocation distro override. It sits exactly where
	// local.star's default_distro_override does in the cascade
	// (image.distro -> override -> defaults.distro), so for a same-named
	// image across distros it selects which variant builds — without
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
	// no name — build everything), fall back to the project default.
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

func projectDir() string {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func cmdContainer(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s container <build|shell|status|binfmt>\n", os.Args[0])
		os.Exit(1)
	}

	switch args[0] {
	case "build":
		fmt.Println("Containers are now units. Use: osb build toolchain-musl")
	case "shell":
		cmdContainerShell()
	case "status":
		fmt.Println("Containers are now units. Use: osb describe toolchain-musl")
	case "binfmt":
		fmt.Println("This will register QEMU user-mode emulation for foreign architectures")
		fmt.Println("by running a privileged Docker container (tonistiigi/binfmt).")
		fmt.Println()
		fmt.Println("This enables building arm64 and riscv64 images on your " + build.Arch() + " host.")
		fmt.Println("The registration persists until reboot.")
		fmt.Println()
		fmt.Print("Proceed? (y/n) ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Cancelled.")
			return
		}
		if err := osb.RegisterBinfmt(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown container subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdContainerShell() {
	projectDir := projectDir()
	sysroot := filepath.Join(projectDir, "build", build.Arch(), "shell", "sysroot")
	build.EnsureDir(sysroot)

	// Use a temp dir for src/destdir so the sandbox mounts are valid
	srcDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "src")
	destDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "destdir")
	build.EnsureDir(srcDir)
	build.EnsureDir(destDir)

	cfg := &build.SandboxConfig{
		Sandbox:    true,
		Shell:      "bash",
		SrcDir:     srcDir,
		DestDir:    destDir,
		Sysroot:    sysroot,
		ProjectDir: projectDir,
		Env: map[string]string{
			"PREFIX":          "/usr",
			"DESTDIR":         "/build/destdir",
			"NPROC":           build.NProc(),
			"ARCH":            build.Arch(),
			"HOME":            "/tmp",
			"PATH":            "/build/sysroot/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"PKG_CONFIG_PATH": "/build/sysroot/usr/lib/pkgconfig:/usr/lib/pkgconfig",
			"CFLAGS":          "-I/build/sysroot/usr/include",
			"CPPFLAGS":        "-I/build/sysroot/usr/include",
			"LDFLAGS":         "-L/build/sysroot/usr/lib",
			"PYTHONPATH":      "/build/sysroot/usr/lib/python3.12/site-packages",
		},
	}

	bwrapCmd := build.BwrapShellCommand(cfg)
	mounts := []osb.Mount{
		{Host: srcDir, Container: "/build/src"},
		{Host: destDir, Container: "/build/destdir"},
		{Host: sysroot, Container: "/build/sysroot", ReadOnly: true},
	}

	// Resolve container image from project
	proj := loadProject()

	if err := osb.RunInContainer(osb.ContainerRunConfig{
		Shell:       "bash",
		Image:       osb.DefaultContainerImage(proj),
		Command:     bwrapCmd,
		ProjectDir:  projectDir,
		Mounts:      mounts,
		Interactive: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	machine := fs.String("machine", "", "default machine for the project")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s init <project-dir> [--machine <name>]\n", os.Args[0])
		os.Exit(1)
	}

	if err := osb.RunInit(fs.Arg(0), *machine); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdConfig(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s config <show|set> [...]\n", os.Args[0])
		os.Exit(1)
	}

	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}

	switch args[0] {
	case "show":
		if err := osb.ShowConfig(dir, os.Stdout, projectLoadOpts()...); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "set":
		// local.star is osb-generated and safe to rewrite, so the
		// per-developer settings that live there are settable from the
		// CLI. PROJECT.star is hand-authored Starlark and is not.
		if len(args) == 3 && args[1] == "parallel-builds" {
			n, err := strconv.Atoi(args[2])
			if err != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "config set parallel-builds: value must be an integer >= 1\n")
				os.Exit(1)
			}
			root, err := findProjectRootForLocal(projectDir())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			ov, _ := osbstar.LoadLocalOverrides(root)
			ov.ParallelBuilds = n
			if err := osbstar.WriteLocalOverrides(root, ov); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("parallel-builds = %d (saved to local.star)\n", n)
			return
		}
		if len(args) == 3 && args[1] == "qemu-memory" {
			mem := strings.TrimSpace(args[2])
			root, err := findProjectRootForLocal(projectDir())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			ov, _ := osbstar.LoadLocalOverrides(root)
			ov.QEMUMemory = mem // empty string clears it — machine default reapplies
			if err := osbstar.WriteLocalOverrides(root, ov); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if mem == "" {
				fmt.Printf("qemu-memory cleared from local.star; the machine default applies\n")
			} else {
				fmt.Printf("qemu-memory = %s (saved to local.star)\n", mem)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "config set: supported keys are 'parallel-builds <n>' and 'qemu-memory <size>'; edit PROJECT.star directly for project config\n")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

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

func loadProject() *osbstar.Project {
	return loadProjectWithMachine("")
}

// tryLoadProject returns nil if no project is loadable from the cwd
// (rather than os.Exit'ing like loadProject). Useful for commands that
// can run inside or outside a project, like `osb device repo list`.
// projectLoadOpts returns the LoadOptions derived from global CLI flags. The
// TUI also needs these so reloads (after editing .star files or switching
// machines) honor flags like --allow-duplicate-provides.
func projectLoadOpts() []osbstar.LoadOption {
	opts := []osbstar.LoadOption{
		osbstar.WithModuleSync(module.SyncIfNeeded),
		osbstar.WithShowShadows(globalShowShadows),
		osbstar.WithAllowDuplicateProvides(globalAllowDuplicateProvides),
		osbstar.WithBuiltin("alpine_feed", alpine.Builtin),
		osbstar.WithBuiltin("apt_feed", apt.Builtin),
	}
	if refs := stdlibModules(); len(refs) > 0 {
		opts = append(opts, osbstar.WithImplicitModules(refs))
	}
	if globalProjectFile != "" {
		opts = append(opts, osbstar.WithProjectFile(globalProjectFile))
	}
	return opts
}

// stdlibPriority ranks the bundled modules from lowest to highest priority.
// Later entries win under the loader's last-wins rule, so the distro feeds sit
// below the core recipes (a source-built module-core unit shadows a same-named
// feed entry) and module-core sits highest, matching the layering osb ships.
var stdlibPriority = []string{
	"module-alpine", "module-debian", "module-ubuntu",
	"module-core",
}

// stdlibModules materializes osb's embedded standard library and returns it as
// module references in priority order, ready to inject via WithImplicitModules.
// Any materialized module not named in stdlibPriority is appended after the
// ranked ones (just below module-core) so a newly added bundled module still
// resolves without a code change here. A materialization failure is reported
// and treated as "no bundled modules", leaving the resulting build to fail with
// a clear missing-unit error rather than a cryptic one.
func stdlibModules() []osbstar.ModuleRef {
	dir, names, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not materialize bundled modules: %v\n", err)
		return nil
	}

	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}

	var ordered []string
	ranked := make(map[string]bool, len(stdlibPriority))
	for _, n := range stdlibPriority {
		if present[n] {
			ordered = append(ordered, n)
			ranked[n] = true
		}
	}
	for _, n := range names {
		if !ranked[n] {
			ordered = append(ordered, n)
		}
	}

	refs := make([]osbstar.ModuleRef, 0, len(ordered))
	for _, n := range ordered {
		refs = append(refs, osbstar.ModuleRef{
			URL:   "osb.stdlib/" + n,
			Local: stdlib.ModulePath(dir, n),
		})
	}
	return refs
}

// alpineArchDir and debArchDir map an osb-canonical arch to the per-arch
// subdirectory each distro's mirror uses.
func alpineArchDir(arch string) string {
	if arch == "arm64" {
		return "aarch64"
	}
	return arch // x86_64
}

func debArchDir(arch string) string {
	if arch == "arm64" {
		return "arm64"
	}
	return "amd64" // x86_64
}

// archHint guesses the target arches to prepare feed indexes for before the
// project is loaded (the real target arch is only known after evaluation, which
// itself needs the indexes). A machine name mentioning arm64/aarch64 narrows to
// arm64; anything else — including the empty default — prepares both, which is
// cheap for Alpine's small index.
func archHint(machineName string) []string {
	m := strings.ToLower(machineName)
	if strings.Contains(m, "arm64") || strings.Contains(m, "aarch64") {
		return []string{"arm64"}
	}
	if strings.Contains(m, "x86_64") || strings.Contains(m, "amd64") {
		return []string{"x86_64"}
	}
	return []string{"x86_64", "arm64"}
}

// ensureStdlibFeeds fetches the bundled feed indexes a build needs but that are
// stripped from the embedded stdlib (see internal/stdlib). It fetches only what
// is missing, so it costs one network round per feed+arch on a cold cache and
// nothing afterwards. Alpine's small index is always ensured (images across the
// bundled modules evaluate it); the much larger apt indexes are fetched only
// for the distro actually being built. Failures are reported but not fatal —
// the load that follows fails with a clear missing-index message if an index is
// genuinely required and could not be fetched.
func ensureStdlibFeeds(distro string, arches []string) {
	dir, _, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		return
	}
	// The bundled Alpine images are evaluated at project load regardless of the
	// build's own distro, so the Alpine index is always required.
	ensureAlpineIndex(stdlib.ModulePath(dir, "module-alpine"), arches)
	// An apt build's closure walk probes its sibling apt distro for packages
	// that exist in both (e.g. python3.11) before the distro filter rejects the
	// cross-distro hit, so both apt indexes must be present, not just the
	// target's. Alpine builds never reach the apt feeds and skip this.
	if distro == "debian" || distro == "ubuntu" {
		ensureAptIndex(stdlib.ModulePath(dir, "module-debian"), arches)
		ensureAptIndex(stdlib.ModulePath(dir, "module-ubuntu"), arches)
	}
}

// effectiveDistroHint returns the distro a build/run will target: the explicit
// --distro flag when set, otherwise the project's defaults.distro read directly
// from PROJECT.star. It is a pre-load hint used only to decide which bundled
// feed indexes to prepare; the authoritative distro cascade still runs during
// evaluation.
func effectiveDistroHint(flagDistro string) string {
	if flagDistro != "" {
		return flagDistro
	}
	projFile := globalProjectFile
	if projFile == "" {
		projFile = filepath.Join(projectDir(), "PROJECT.star")
	}
	data, err := os.ReadFile(projFile)
	if err != nil {
		return ""
	}
	// Match the distro inside the defaults(...) call. The prefer_modules block
	// uses distro names as map keys ("alpine":) rather than `distro =`, so a
	// simple `distro = "<name>"` match lands on the defaults entry.
	m := regexp.MustCompile(`(?s)defaults\s*\(.*?distro\s*=\s*"([a-z0-9]+)"`).FindSubmatch(data)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// ensureAlpineIndex fetches the Alpine APKINDEX for any of arches whose primary
// (main) index is not already present under moduleDir.
func ensureAlpineIndex(moduleDir string, arches []string) {
	var missing []string
	for _, a := range arches {
		idx := filepath.Join(moduleDir, "feeds", "main", alpineArchDir(a), "APKINDEX")
		if _, err := os.Stat(idx); err != nil {
			missing = append(missing, a)
		}
	}
	if len(missing) == 0 {
		return
	}
	if err := alpine.UpdateFeeds(alpine.UpdateOptions{ModuleDir: moduleDir, Arches: missing, Out: os.Stdout}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetching Alpine feed index: %v\n", err)
	}
}

// ensureAptIndex fetches the apt Packages index for any of arches whose main
// component index is not already present under moduleDir.
func ensureAptIndex(moduleDir string, arches []string) {
	var missing []string
	for _, a := range arches {
		idx := filepath.Join(moduleDir, "feeds", "main", debArchDir(a), "Packages")
		if _, err := os.Stat(idx); err != nil {
			missing = append(missing, a)
		}
	}
	if len(missing) == 0 {
		return
	}
	if err := apt.UpdateFeeds(apt.UpdateOptions{ModuleDir: moduleDir, Arches: missing, Out: os.Stdout}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetching apt feed index: %v\n", err)
	}
}

// globalFlagArgs returns the global flags as argv tokens, suitable for
// prepending to a re-exec of the osb binary so the child inherits the same
// load behavior as the parent (TUI re-execs `osb run` for image launches).
func globalFlagArgs() []string {
	var args []string
	if globalProjectFile != "" {
		args = append(args, "--project", globalProjectFile)
	}
	if globalShowShadows {
		args = append(args, "--show-shadows")
	}
	if globalAllowDuplicateProvides {
		args = append(args, "--allow-duplicate-provides")
	}
	return args
}

func tryLoadProject() *osbstar.Project {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}
	proj, err := osbstar.LoadProject(dir, projectLoadOpts()...)
	if err != nil {
		return nil
	}
	return proj
}

func loadProjectWithMachine(machineName string) *osbstar.Project {
	return loadProjectWithMachineDistro(machineName, "")
}

// loadProjectWithMachineDistro loads the project with an optional --machine
// and --distro override threaded into the loader so both take effect before
// Starlark evaluates units and images. The distro override in particular must
// be set pre-eval: image() resolves its distro_artifacts branch and
// rootfs/disk functions eagerly during evaluation, so patching the override
// onto the returned Project would leave the closure baked against the wrong
// distro.
func loadProjectWithMachineDistro(machineName, distroOverride string) *osbstar.Project {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}
	// Precedence: --machine flag > local.star > PROJECT.star defaults.
	// Local image override is also captured here and applied below — it
	// doesn't affect Starlark eval, so we just patch proj.Defaults.Image.
	var ovImage string
	if machineName == "" {
		absDir, err := filepath.Abs(dir)
		if err == nil {
			if root, err := findProjectRootForLocal(absDir); err == nil {
				if ov, err := osbstar.LoadLocalOverrides(root); err == nil {
					machineName = ov.Machine
					ovImage = ov.Image
				} else {
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				}
			}
		}
	}
	opts := projectLoadOpts()
	if machineName != "" {
		opts = append(opts, osbstar.WithMachine(machineName))
	}
	if distroOverride != "" {
		opts = append(opts, osbstar.WithDistroOverride(distroOverride))
	}
	proj, err := osbstar.LoadProject(dir, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if ovImage != "" {
		if proj.AnyUnit(ovImage) != nil {
			proj.Defaults.Image = ovImage
		} else {
			fmt.Fprintf(os.Stderr, "Warning: local.star image %q not found in project; ignoring\n", ovImage)
		}
	}
	return proj
}

// findProjectRootForLocal walks up from dir looking for PROJECT.star so
// LoadLocalOverrides can be called against the project root (where
// local.star lives) rather than the working dir.
func findProjectRootForLocal(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "PROJECT.star")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no PROJECT.star in %s or parents", dir)
		}
		dir = parent
	}
}

// unitBuildDirForCWD resolves the build directory for a named unit in the
// current project, for CLI subcommands that navigate the build tree (e.g.
// `osb log`, `osb diagnose`). It mirrors how the TUI locates a unit's build
// dir so the CLI and TUI agree, resolving two things the build path also
// resolves:
//
//   - the effective distro (build/<distro>/), honoring the cascade
//     image.distro -> local.star default_distro_override -> defaults.distro;
//   - the unit's build scope (<name>.<scopeDir>), where arch-scoped units use
//     the arch and machine-scoped units — images, kernels — use the machine.
//
// Hardcoding the arch (the old behavior) never found a machine-scoped unit's
// log, and loading the project without projectLoadOpts() crashed with
// "undefined: alpine_feed" on any project whose modules declare a feed.
func unitBuildDirForCWD(dir, unitName string) (string, error) {
	opts := projectLoadOpts()
	// Honor the developer's local.star machine/distro override so we navigate
	// the same build/<distro>/<name>.<scope>/ subtree `osb build` wrote to.
	var ov osbstar.LocalOverrides
	if absDir, aerr := filepath.Abs(dir); aerr == nil {
		if root, rerr := findProjectRootForLocal(absDir); rerr == nil {
			if loaded, lerr := osbstar.LoadLocalOverrides(root); lerr == nil {
				ov = loaded
			}
		}
	}
	machine := ov.Machine
	if machine != "" {
		opts = append(opts, osbstar.WithMachine(machine))
	}
	proj, err := osbstar.LoadProject(dir, opts...)
	if err != nil {
		return "", fmt.Errorf("loading project to resolve distro: %w", err)
	}
	if ov.DefaultDistroOverride != "" {
		proj.DefaultDistroOverride = ov.DefaultDistroOverride
	}
	if machine == "" {
		machine = proj.Defaults.Machine
	}
	arch, err := resolveTargetArch(proj, machine)
	if err != nil {
		return "", err
	}
	// An image may pin its own distro; fall back to the project-level
	// effective distro for non-image units.
	distro, err := proj.EffectiveDistroForImage(unitName)
	if err != nil {
		if distro, err = proj.EffectiveDistro(); err != nil {
			return "", err
		}
	}
	scopeDir := arch
	if u := proj.LookupUnit(distro, unitName); u != nil {
		scopeDir = build.ScopeDir(u, arch, machine)
	}
	return build.UnitBuildDir(dir, scopeDir, unitName, distro), nil
}

func defaultArch(proj *osbstar.Project) string {
	if m, ok := proj.Machines[proj.Defaults.Machine]; ok {
		return m.Arch
	}
	// Fallback: pick the first machine's arch
	for _, m := range proj.Machines {
		return m.Arch
	}
	return "unknown"
}

func cmdDesc(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s desc <unit>\n", os.Args[0])
		os.Exit(1)
	}
	proj := loadProject()
	arch := defaultArch(proj)
	if err := resolve.Describe(os.Stdout, proj, args[0], arch); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdRefs(args []string) {
	fs := flag.NewFlagSet("refs", flag.ExitOnError)
	direct := fs.Bool("direct", false, "show only direct dependents")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s refs <unit> [--direct]\n", os.Args[0])
		os.Exit(1)
	}

	proj := loadProject()
	if err := resolve.Refs(os.Stdout, proj, fs.Arg(0), *direct); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdGraph(args []string) {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	format := fs.String("format", "text", "output format (text, dot)")
	fs.Parse(args)

	filter := fs.Arg(0)

	proj := loadProject()
	if err := resolve.Graph(os.Stdout, proj, *format, filter); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	edit := fs.Bool("e", false, "open log in editor")
	fs.Parse(args)

	dir := projectDir()
	unitName := fs.Arg(0)
	var logPath string

	if unitName != "" {
		buildDir, derr := unitBuildDirForCWD(dir, unitName)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", derr)
			os.Exit(1)
		}
		logPath = filepath.Join(buildDir, "build.log")
	} else {
		logPath = findLatestBuildLog(dir)
	}

	if logPath == "" {
		fmt.Fprintln(os.Stderr, "No build logs found")
		os.Exit(1)
	}

	if *edit {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, logPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}

// findLatestBuildLog returns the newest build.log under build/<arch>/, or "" if
// none exist. Used by `osb log` with no unit argument.
func findLatestBuildLog(projectDir string) string {
	archDir := filepath.Join(projectDir, "build", build.Arch())
	entries, err := os.ReadDir(archDir)
	if err != nil {
		return ""
	}

	type logEntry struct {
		path    string
		modTime int64
	}
	var logs []logEntry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(archDir, e.Name(), "build.log")
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		logs = append(logs, logEntry{p, info.ModTime().UnixNano()})
	}

	if len(logs) == 0 {
		return ""
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].modTime > logs[j].modTime
	})
	return logs[0].path
}

func cmdFlash(args []string) {
	if len(args) > 0 && args[0] == "list" {
		cmdFlashList(args[1:])
		return
	}

	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	machineName := fs.String("machine", "", "target machine")
	dryRun := fs.Bool("dry-run", false, "show what would be flashed without writing")
	assumeYes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s flash <image-unit> <device> [--machine <name>] [--yes] [--dry-run]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s flash list\n", os.Args[0])
		os.Exit(1)
	}

	unitName := fs.Arg(0)
	devicePath := fs.Arg(1)

	if devicePath == "" && !*dryRun {
		fmt.Fprintf(os.Stderr, "Usage: %s flash <image-unit> <device>\n", os.Args[0])
		os.Exit(1)
	}

	proj := loadProjectWithMachine(*machineName)
	if err := device.Flash(proj, unitName, devicePath, projectDir(), *dryRun, *assumeYes, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdFlashList(_ []string) {
	cands, err := device.ListCandidates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(cands) == 0 {
		fmt.Println("No removable devices detected.")
		return
	}
	fmt.Printf("%-14s %8s  %-4s %-10s %s\n", "DEVICE", "SIZE", "BUS", "VENDOR", "MODEL")
	for _, c := range cands {
		fmt.Printf("%-14s %8s  %-4s %-10s %s\n",
			c.Path, device.FormatSize(c.Size), c.Bus, c.Vendor, c.Model)
	}
}

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
	// partition to expand into — enough to exercise the grow path and
	// hold the Docker image cache during on-target work. Pass an empty
	// string to disable and run against disk.img directly.
	diskSize := fs.String("disk-size", "8G", "grow QEMU disk image to this size for the run (empty to disable)")
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

func cmdRepo(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s repo <list|info|remove|clean> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	proj := loadProject()
	// `osb repo list/info/remove/clean` operates on the per-distro
	// subtree at repo/<project>/<distro>/. Use the project's
	// effective distro; cross-distro queries would walk each distro
	// subtree separately.
	distro, err := proj.EffectiveDistro()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	repoDir := repo.RepoDistroDir(proj, projectDir(), distro)

	switch args[0] {
	case "list":
		if err := repo.List(repoDir, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "info":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s repo info <package>\n", os.Args[0])
			os.Exit(1)
		}
		if err := repo.Info(repoDir, args[1], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "remove":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: %s repo remove <package>\n", os.Args[0])
			os.Exit(1)
		}
		// Load the project's signing key so the regenerated APKINDEX stays
		// signed. Failure here is fatal — an unsigned index would silently
		// break apk add against this repo.
		signer, err := artifact.LoadOrGenerateSigner(proj.Name, proj.SigningKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: loading signing key: %v\n", err)
			os.Exit(1)
		}
		if err := repo.Remove(repoDir, args[1], signer, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "clean":
		// Drops .apk files no current unit produces, then re-signs the
		// regenerated APKINDEX. Same signer concern as `remove`.
		signer, err := artifact.LoadOrGenerateSigner(proj.Name, proj.SigningKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: loading signing key: %v\n", err)
			os.Exit(1)
		}
		if err := repo.Clean(proj, repoDir, signer, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown repo subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

// Returns true if the command was found and executed.
func tryCustomCommand(command string, args []string) bool {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}

	cmds, engines, err := osbstar.LoadCommands(dir)
	if err != nil {
		// No commands directory or eval error — not a custom command
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
