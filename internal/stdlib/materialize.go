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

const Root = "stdlib"

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

func ModulePath(dir, module string) string {
	return filepath.Join(dir, path.Clean(module))
}
