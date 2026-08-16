package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	embedded "github.com/anhhao17/osb"
	"github.com/anhhao17/osb/internal/build"
	"github.com/anhhao17/osb/internal/feeds/alpine"
	"github.com/anhhao17/osb/internal/feeds/apt"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"github.com/anhhao17/osb/internal/stdlib"
)

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
// arm64; anything else - including the empty default - prepares both, which is
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
// for the distro actually being built. Failures are reported but not fatal -
// the load that follows fails with a clear missing-index message if an index is
// genuinely required and could not be fetched.
func ensureStdlibFeeds(distro string, arches []string) {
	dir, _, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		return
	}
	// Every feed index is needed regardless of the target distro: images
	// declare artifacts for all three distros, and the closure walk enumerates a
	// shared virtual's providers (e.g. "linux") across every sibling feed. A
	// missing index there aborts the resolve. Indexes are fetched once per arch
	// and cached in the materialized stdlib, so this is a cold-cache cost only.
	_ = distro
	ensureAlpineIndex(stdlib.ModulePath(dir, "module-alpine"), arches)
	ensureAptIndex(stdlib.ModulePath(dir, "module-debian"), arches)
	ensureAptIndex(stdlib.ModulePath(dir, "module-ubuntu"), arches)
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
	// Local image override is also captured here and applied below - it
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
//     the arch and machine-scoped units - images, kernels - use the machine.
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

// stdlibPriority ranks the bundled modules from lowest to highest priority.
// Later entries win under the loader's last-wins rule, so the distro feeds sit
// below the core recipes (a source-built module-core unit shadows a same-named
// feed entry) and module-core sits highest, matching the layering osb ships.
var stdlibPriority = []string{
	"module-alpine", "module-debian", "module-ubuntu",
	"module-core",
}
