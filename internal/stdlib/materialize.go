// Package stdlib materializes osb's embedded standard-library modules to a
// per-user cache directory so the loader can resolve them as ordinary local
// modules.
//
// The modules are shipped inside the binary (see the repository-root embedded
// package). Materialize writes them to disk once, content-addressed, and hands
// back the directory and module names; cmd/osb turns those into implicit
// lowest-priority module references injected at project load.
package stdlib

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Root is the directory name the embedded modules live under, both inside the
// embed.FS and in the materialized cache tree.
const Root = "stdlib"

// Materialize extracts the embedded stdlib tree from src to a stable per-user
// cache directory and returns that directory together with the names of the
// module subdirectories it contains (e.g. "module-core", "module-alpine").
//
// Extraction is content-addressed and idempotent: the tree is written under
// <user-cache>/osb/stdlib/<digest>/, where <digest> is a hash of every
// embedded file's path and contents. A given osb binary therefore always
// resolves to the same directory, a rebuilt binary with changed modules gets a
// fresh one, and repeated calls after the first are a cheap stat. A completion
// marker guards against a half-written tree from an interrupted run.
func Materialize(src fs.FS) (dir string, modules []string, err error) {
	digest, err := hashTree(src)
	if err != nil {
		return "", nil, fmt.Errorf("hashing embedded stdlib: %w", err)
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("locating user cache dir: %w", err)
	}
	dir = filepath.Join(cache, "osb", "stdlib", digest)
	marker := filepath.Join(dir, ".complete")

	if _, statErr := os.Stat(marker); statErr != nil {
		if err := extract(src, dir, marker); err != nil {
			return "", nil, err
		}
	}

	modules, err = moduleNames(dir)
	if err != nil {
		return "", nil, err
	}
	return dir, modules, nil
}

// hashTree returns a hex digest over every file's path and contents under
// src's Root, walked in a stable order so the digest is deterministic.
func hashTree(src fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(src, Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s\x00", p)
		f, err := src.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// extract writes src's Root tree into dir atomically: it renders to a sibling
// temporary directory, then renames it into place and drops the completion
// marker. A leftover partial dir from a prior crash is removed first.
func extract(src fs.FS, dir, marker string) error {
	_ = os.RemoveAll(dir)
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}

	err := fs.WalkDir(src, Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// p is "stdlib/..."; strip the Root prefix so the tree lands directly
		// under tmp with module-* at its top level.
		rel := p[len(Root):]
		dst := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		return copyFile(src, p, dst)
	})
	if err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("extracting embedded stdlib: %w", err)
	}

	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("installing stdlib cache: %w", err)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return fmt.Errorf("writing stdlib completion marker: %w", err)
	}
	return nil
}

// copyFile copies a single embedded file at srcPath to dst on disk.
func copyFile(src fs.FS, srcPath, dst string) error {
	in, err := src.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// moduleNames lists the module subdirectories of a materialized stdlib dir,
// sorted, so the caller injects them in a deterministic order.
func moduleNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading stdlib cache dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ModulePath joins a materialized stdlib dir and a module name into the module
// directory the loader resolves. It is a thin helper over path/filepath so
// callers do not hard-code the layout.
func ModulePath(dir, module string) string {
	return filepath.Join(dir, path.Clean(module))
}
