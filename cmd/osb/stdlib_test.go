package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	embedded "github.com/anhhao17/osb"
	"github.com/anhhao17/osb/internal/stdlib"
)

// TestBundledMachines verifies the embedded standard library materializes and
// ships the machines osb targets - including the UEFI and Secure Boot ones,
// which must resolve to x86_64 (a past bug shipped a broken arm64 stub for
// non-default machine names). No network: it inspects the materialized tree.
func TestBundledMachines(t *testing.T) {
	dir, names, err := stdlib.Materialize(embedded.StdlibFS)
	if err != nil {
		t.Fatalf("materialize stdlib: %v", err)
	}
	if !contains(names, "module-core") {
		t.Fatalf("bundled stdlib missing module-core; got %v", names)
	}

	machinesDir := filepath.Join(dir, "module-core", "machines")
	want := map[string][]string{
		"qemu-x86_64":      {`arch = "x86_64"`, `console = "ttyS0"`},
		"qemu-x86_64-bios": {`arch = "x86_64"`, `firmware = "bios"`},
		"qemu-arm64":       {`arch = "arm64"`, `console = "ttyAMA0"`},
		"x86_64":           {`arch = "x86_64"`},
		"arm64":            {`arch = "arm64"`},
	}
	for machine, needles := range want {
		data, err := os.ReadFile(filepath.Join(machinesDir, machine+".star"))
		if err != nil {
			t.Errorf("bundled machine %q missing: %v", machine, err)
			continue
		}
		for _, n := range needles {
			if !strings.Contains(string(data), n) {
				t.Errorf("machine %q: expected %s", machine, n)
			}
		}
		for _, distro := range []string{"alpine", "debian", "ubuntu"} {
			if !strings.Contains(string(data), `"`+distro+`":`) {
				t.Errorf("machine %q has no kernel for %s", machine, distro)
			}
		}
	}

	refs := stdlibModules()
	if len(refs) == 0 {
		t.Fatal("stdlibModules returned no module references")
	}
	if last := refs[len(refs)-1].URL; !strings.HasSuffix(last, "module-core") {
		t.Errorf("expected module-core last (highest priority); got %q", last)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
