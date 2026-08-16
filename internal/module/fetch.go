package module

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

// CacheDir returns the module cache directory.
// Defaults to cache/modules/ in the current working directory.
func CacheDir() (string, error) {
	dir := os.Getenv("OSB_CACHE")
	if dir == "" {
		dir = "cache"
	}
	dir = filepath.Join(dir, "modules")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// SyncIfNeeded clones any modules that are not already cached. Unlike Sync,
// it does not fetch/update modules that already exist - keeping it fast enough
// to call on every build without adding latency.
func SyncIfNeeded(modules []osbstar.ModuleRef, w io.Writer) error {
	cacheDir, err := CacheDir()
	if err != nil {
		return err
	}

	for _, m := range modules {
		if m.Local != "" {
			continue
		}

		name := ModuleName(m)
		moduleDir := filepath.Join(cacheDir, name)

		if _, err := os.Stat(filepath.Join(moduleDir, ".git")); err == nil {
			continue // already cloned
		}

		ref := m.Ref
		if ref == "" {
			ref = "main"
		}

		fmt.Fprintf(w, "[osb] cloning module %s (ref: %s)...\n", name, ref)
		cmd := exec.Command("git", "clone", "--depth", "1", "--branch", ref, m.URL, moduleDir)
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cloning module %s: %w", name, err)
		}
	}

	return nil
}

// ModuleName derives the module name from a ModuleRef.
// If Path is set, uses the last component of Path (e.g., "modules/module-core" -> "module-core").
// Otherwise uses the last component of URL (e.g., "github.com/osb/module-core" -> "module-core").
func ModuleName(m osbstar.ModuleRef) string {
	if m.Path != "" {
		return filepath.Base(m.Path)
	}
	url := strings.TrimSuffix(m.URL, ".git")
	return filepath.Base(url)
}
