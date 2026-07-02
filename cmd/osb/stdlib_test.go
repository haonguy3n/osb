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
// ships the machines osb targets — including the UEFI and Secure Boot ones,
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
	// Every machine osb ships, with the arch it must resolve to.
	want := map[string]string{
		"qemu-x86_64":                        "x86_64",
		"qemu-arm64":                         "arm64",
		"qemu-x86_64-uefi":                   "x86_64",
		"qemu-x86_64-uefi-secureboot":        "x86_64",
		"qemu-x86_64-uefi-secureboot-verity": "x86_64",
		"qemu-arm64-uefi-secureboot":         "arm64",
		"qemu-arm64-uefi-secureboot-verity":  "arm64",
		"qemu-x86_64-uefi-ab":                "x86_64",
		"x86_64":                             "x86_64",
	}
	for machine, arch := range want {
		data, err := os.ReadFile(filepath.Join(machinesDir, machine+".star"))
		if err != nil {
			t.Errorf("bundled machine %q missing: %v", machine, err)
			continue
		}
		if !strings.Contains(string(data), `arch = "`+arch+`"`) {
			t.Errorf("machine %q: expected arch = %q", machine, arch)
		}
	}

	// The Secure Boot machine must actually enable Secure Boot.
	sb, err := os.ReadFile(filepath.Join(machinesDir, "qemu-x86_64-uefi-secureboot.star"))
	if err != nil {
		t.Fatalf("reading secureboot machine: %v", err)
	}
	if !strings.Contains(string(sb), "secure_boot = True") {
		t.Error("qemu-x86_64-uefi-secureboot must set secure_boot = True")
	}

	// The bundled modules must inject in priority order with module-core last.
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
