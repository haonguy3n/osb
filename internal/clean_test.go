package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunClean_Default(t *testing.T) {
	proj := t.TempDir()
	buildDir := filepath.Join(proj, "build")
	repoDir := filepath.Join(proj, "repo")

	for _, d := range []string{
		filepath.Join(buildDir, "foo"),
		filepath.Join(repoDir, "bar"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunClean(proj, false, nil); err != nil {
		t.Fatalf("RunClean default: %v", err)
	}

	if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
		t.Error("expected build dir to be removed")
	}
	if _, err := os.Stat(repoDir); err != nil {
		t.Error("expected repo dir to still exist")
	}
}

func TestRunClean_All(t *testing.T) {
	proj := t.TempDir()
	buildDir := filepath.Join(proj, "build")
	repoDir := filepath.Join(proj, "repo")

	for _, d := range []string{buildDir, repoDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunClean(proj, true, nil); err != nil {
		t.Fatalf("RunClean all: %v", err)
	}

	for _, d := range []string{buildDir, repoDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", d)
		}
	}
}

func TestRunClean_Units(t *testing.T) {
	proj := t.TempDir()
	buildDir := filepath.Join(proj, "build")

	for _, distro := range []string{"alpine", "debian"} {
		for _, r := range []string{"openssl", "busybox"} {
			if err := os.MkdirAll(filepath.Join(buildDir, distro, r+".x86_64"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := RunClean(proj, false, []string{"openssl"}); err != nil {
		t.Fatalf("RunClean units: %v", err)
	}

	for _, distro := range []string{"alpine", "debian"} {
		if _, err := os.Stat(filepath.Join(buildDir, distro, "openssl.x86_64")); !os.IsNotExist(err) {
			t.Errorf("expected %s/openssl build dir to be removed", distro)
		}
		if _, err := os.Stat(filepath.Join(buildDir, distro, "busybox.x86_64")); err != nil {
			t.Errorf("expected %s/busybox build dir to still exist", distro)
		}
	}
}

func TestRunClean_NoBuildDir(t *testing.T) {
	proj := t.TempDir()

	if err := RunClean(proj, false, nil); err != nil {
		t.Fatalf("RunClean on missing build dir: %v", err)
	}
}
