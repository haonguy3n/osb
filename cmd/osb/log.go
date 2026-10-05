package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anhhao17/osb/internal/build"
)

func cmdLog(args []string) {
	var path string
	if len(args) > 0 {
		proj := loadProject("", "")
		m := proj.Machines[proj.Defaults.Machine]
		distro, err := proj.EffectiveDistroForImage(args[0])
		if err != nil {
			distro, err = proj.EffectiveDistro()
			fail(err)
		}
		scope := m.Arch
		if u := proj.LookupUnit(distro, args[0]); u != nil {
			scope = build.ScopeDir(u, m.Arch, m.Name)
		}
		path = filepath.Join(build.UnitBuildDir(projectDir(), scope, args[0], distro), "build.log")
	} else {
		logs, _ := filepath.Glob(filepath.Join(projectDir(), "build", "*", "*", "build.log"))
		var newest os.FileInfo
		for _, l := range logs {
			if info, err := os.Stat(l); err == nil && (newest == nil || info.ModTime().After(newest.ModTime())) {
				newest, path = info, l
			}
		}
	}
	data, err := os.ReadFile(path)
	if path == "" || err != nil {
		fmt.Fprintln(os.Stderr, "no build log found")
		os.Exit(1)
	}
	os.Stdout.Write(data)
}
