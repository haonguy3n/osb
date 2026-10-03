package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	embedded "github.com/anhhao17/osb"
	"github.com/anhhao17/osb/internal/feeds/alpine"
	"github.com/anhhao17/osb/internal/feeds/apt"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"github.com/anhhao17/osb/internal/stdlib"
)

var stdlibPriority = []string{"module-alpine", "module-debian", "module-ubuntu", "module-core"}

func projectDir() string {
	dir := os.Getenv("OSB_PROJECT")
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func loadProject(machine, distro string) *osbstar.Project {
	dir := projectDir()
	ensureFeeds(dir, distro, machine)
	opts := []osbstar.LoadOption{
		osbstar.WithAllowDuplicateProvides(true),
		osbstar.WithBuiltin("alpine_feed", alpine.Builtin),
		osbstar.WithBuiltin("apt_feed", apt.Builtin),
	}
	if refs := stdlibModules(); len(refs) > 0 {
		opts = append(opts, osbstar.WithImplicitModules(refs))
	}
	if machine != "" {
		opts = append(opts, osbstar.WithMachine(machine))
	}
	if distro != "" {
		opts = append(opts, osbstar.WithDistroOverride(distro))
	}
	proj, err := osbstar.LoadProject(dir, opts...)
	fail(err)
	if _, ok := proj.Machines[proj.Defaults.Machine]; !ok {
		fail(fmt.Errorf("machine %q not found", proj.Defaults.Machine))
	}
	return proj
}

func stdlibModules() []osbstar.ModuleRef {
	dir, names, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not materialize the bundled modules: %v\n", err)
		return nil
	}
	present := map[string]bool{}
	for _, n := range names {
		present[n] = true
	}
	var refs []osbstar.ModuleRef
	for _, n := range stdlibPriority {
		if present[n] {
			refs = append(refs, osbstar.ModuleRef{URL: "osb.stdlib/" + n, Local: stdlib.ModulePath(dir, n)})
			delete(present, n)
		}
	}
	for _, n := range names {
		if present[n] {
			refs = append(refs, osbstar.ModuleRef{URL: "osb.stdlib/" + n, Local: stdlib.ModulePath(dir, n)})
		}
	}
	return refs
}

func ensureFeeds(projectDir, distro, machine string) {
	dir, _, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		return
	}
	if distro == "" {
		if data, err := os.ReadFile(filepath.Join(projectDir, "PROJECT.star")); err == nil {
			if m := regexp.MustCompile(`(?s)defaults\s*\(.*?distro\s*=\s*"([a-z0-9]+)"`).FindSubmatch(data); m != nil {
				distro = string(m[1])
			}
		}
	}
	arches := []string{"x86_64", "arm64"}
	if m := strings.ToLower(machine); strings.Contains(m, "arm64") || strings.Contains(m, "aarch64") {
		arches = []string{"arm64"}
	} else if strings.Contains(m, "x86_64") {
		arches = []string{"x86_64"}
	}
	for _, d := range []string{"alpine", "debian", "ubuntu"} {
		if distro != "" && d != distro {
			continue
		}
		moduleDir := stdlib.ModulePath(dir, "module-"+d)
		var missing []string
		for _, a := range arches {
			idx := filepath.Join(moduleDir, "feeds", "main", feedArch(d, a), "Packages")
			if d == "alpine" {
				idx = filepath.Join(moduleDir, "feeds", "main", feedArch(d, a), "APKINDEX")
			}
			if _, err := os.Stat(idx); err != nil {
				missing = append(missing, a)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if d == "alpine" {
			err = alpine.UpdateFeeds(alpine.UpdateOptions{ModuleDir: moduleDir, Arches: missing, Out: os.Stdout})
		} else {
			err = apt.UpdateFeeds(apt.UpdateOptions{ModuleDir: moduleDir, Arches: missing, Out: os.Stdout})
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: fetching the %s package index: %v\n", d, err)
		}
	}
}

func feedArch(distro, arch string) string {
	switch {
	case distro == "alpine" && arch == "arm64":
		return "aarch64"
	case distro != "alpine" && arch == "x86_64":
		return "amd64"
	}
	return arch
}
