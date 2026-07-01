package internal

import (
	"fmt"
	"io"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func ShowConfig(dir string, w io.Writer, opts ...osbstar.LoadOption) error {
	proj, err := osbstar.LoadProject(dir, opts...)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Project:    %s %s\n", proj.Name, proj.Version)
	fmt.Fprintf(w, "Machine:    %s (default)\n", proj.Defaults.Machine)
	fmt.Fprintf(w, "Image:      %s (default)\n", proj.Defaults.Image)
	fmt.Fprintf(w, "Cache:      %s\n", proj.Cache.Path)

	ov, _ := osbstar.LoadLocalOverrides(dir)

	parallel := osbstar.DefaultParallelBuilds
	parallelNote := "default"
	if ov.ParallelBuilds > 0 {
		parallel = ov.ParallelBuilds
		parallelNote = "local.star"
	}
	fmt.Fprintf(w, "Parallel:   %d (%s)\n", parallel, parallelNote)

	// QEMU memory `osb run` gives the guest: local.star qemu_memory wins,
	// otherwise the default machine's own qemu memory.
	qemuMem, qemuNote := "unset", "machine default"
	if m, ok := proj.Machines[proj.Defaults.Machine]; ok && m.QEMU != nil && m.QEMU.Memory != "" {
		qemuMem, qemuNote = m.QEMU.Memory, "machine default"
	}
	if ov.QEMUMemory != "" {
		qemuMem, qemuNote = ov.QEMUMemory, "local.star"
	}
	fmt.Fprintf(w, "QEMU mem:   %s (%s)\n", qemuMem, qemuNote)

	// Count distinct unit names across modules. AllUnits may yield
	// the same name twice when alpine.main and debian.main both
	// register it, so dedup before counting and printing.
	unitNames := map[string]*osbstar.Unit{}
	for name, u := range proj.AllUnits() {
		if _, ok := unitNames[name]; !ok {
			unitNames[name] = u
		}
	}

	fmt.Fprintf(w, "Machines:   %d defined\n", len(proj.Machines))
	fmt.Fprintf(w, "Units:    %d defined\n", len(unitNames))

	if len(proj.Machines) > 0 {
		fmt.Fprintln(w, "\nMachines:")
		for name, m := range proj.Machines {
			fmt.Fprintf(w, "  %-20s %s\n", name, m.Arch)
		}
	}

	if len(unitNames) > 0 {
		fmt.Fprintln(w, "\nUnits:")
		for name, r := range unitNames {
			fmt.Fprintf(w, "  %-20s [%s] %s\n", name, r.Class, r.Version)
		}
	}

	return nil
}
