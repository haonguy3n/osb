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

func alpineArchDir(arch string) string {
	if arch == "arm64" {
		return "aarch64"
	}
	return arch
}

func debArchDir(arch string) string {
	if arch == "arm64" {
		return "arm64"
	}
	return "amd64"
}

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

func ensureStdlibFeeds(distro string, arches []string) {
	dir, _, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		return
	}
	_ = distro
	ensureAlpineIndex(stdlib.ModulePath(dir, "module-alpine"), arches)
	ensureAptIndex(stdlib.ModulePath(dir, "module-debian"), arches)
	ensureAptIndex(stdlib.ModulePath(dir, "module-ubuntu"), arches)
}

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
	m := regexp.MustCompile(`(?s)defaults\s*\(.*?distro\s*=\s*"([a-z0-9]+)"`).FindSubmatch(data)
	if m == nil {
		return ""
	}
	return string(m[1])
}

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

func loadProjectWithMachineDistro(machineName, distroOverride string) *osbstar.Project {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}
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

func unitBuildDirForCWD(dir, unitName string) (string, error) {
	opts := projectLoadOpts()
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

var stdlibPriority = []string{
	"module-alpine", "module-debian", "module-ubuntu",
	"module-core",
}
