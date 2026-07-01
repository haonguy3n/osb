package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-project")

	if err := RunInit(dir, ""); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	for _, path := range []string{
		"PROJECT.star",
		".gitignore",
		"machines",
		"units",
		"classes",
		"overlays",
	} {
		full := filepath.Join(dir, path)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after init", path)
		}
	}

	// Verify PROJECT.star is valid Starlark
	content, err := os.ReadFile(filepath.Join(dir, "PROJECT.star"))
	if err != nil {
		t.Fatalf("reading PROJECT.star: %v", err)
	}
	if len(content) == 0 {
		t.Error("PROJECT.star is empty")
	}
}

// TestRunInit_WithMachine checks that a machine flag sets defaults.machine and
// does not write a local machine stub — bundled machines resolve from the
// stdlib, and a stub would only shadow them (a past bug emitted a broken arm64
// stub for non-default names).
func TestRunInit_WithMachine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-project")

	if err := RunInit(dir, "qemu-x86_64-uefi"); err != nil {
		t.Fatalf("RunInit with machine: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "PROJECT.star"))
	if err != nil {
		t.Fatalf("reading PROJECT.star: %v", err)
	}
	if !strings.Contains(string(data), `machine = "qemu-x86_64-uefi"`) {
		t.Error("expected defaults.machine = qemu-x86_64-uefi in PROJECT.star")
	}

	entries, err := os.ReadDir(filepath.Join(dir, "machines"))
	if err != nil {
		t.Fatalf("reading machines dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no local machine stub; got %d files", len(entries))
	}
}

func TestRunInit_ExistingProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "PROJECT.star"), []byte("project(name=\"exists\")\n"), 0644)

	if err := RunInit(dir, ""); err == nil {
		t.Fatal("expected error when init into existing project, got nil")
	}
}
