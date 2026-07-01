package main

import (
	"fmt"
	"github.com/anhhao17/osb/internal/feeds/alpine"
	"github.com/anhhao17/osb/internal/feeds/apt"
	"github.com/anhhao17/osb/internal/module"
	yoestar "github.com/anhhao17/osb/internal/starlark"
)

func main() {
	proj, err := yoestar.LoadProject(".",
		yoestar.WithModuleSync(module.SyncIfNeeded),
		yoestar.WithAllowDuplicateProvides(true),
		yoestar.WithBuiltin("alpine_feed", alpine.Builtin),
		yoestar.WithBuiltin("apt_feed", apt.Builtin),
	)
	if err != nil { fmt.Println("ERR:", err); return }
	// Find anything with xz in RuntimeDeps across every registered
	// module — AllUnits iterates UnitsByModule, yielding entries that
	// might shadow each other in a per-distro view.
	for name, u := range proj.AllUnits() {
		for _, d := range u.RuntimeDeps {
			if d == "xz" {
				fmt.Printf("%s (Distro=%q Module=%s) -> xz\n", name, u.Distro, u.Module)
			}
		}
	}
}
