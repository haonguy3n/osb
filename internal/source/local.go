package source

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func LocalDir(unit *osbstar.Unit) string {
	src := unit.Source
	switch {
	case strings.HasPrefix(src, "file://"):
		src = strings.TrimPrefix(src, "file://")
	case strings.HasPrefix(src, "/"), strings.HasPrefix(src, "./"), strings.HasPrefix(src, "../"):
	default:
		return ""
	}
	if !filepath.IsAbs(src) {
		src = filepath.Join(unit.DefinedIn, src)
	}
	return filepath.Clean(src)
}

func HashLocalDir(dir string) string {
	h := sha256.New()
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipLocal(d.Name()) && p != dir {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s:%o\n", rel, info.Mode())
		if info.Mode()&fs.ModeSymlink != 0 {
			t, _ := os.Readlink(p)
			fmt.Fprintf(h, "->%s\n", t)
			continue
		}
		if f, err := os.Open(p); err == nil {
			_, _ = io.Copy(h, f)
			f.Close()
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func skipLocal(name string) bool {
	return name == ".git" || name == "build" || name == ".cache"
}

func copyLocalDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && rel != "." && skipLocal(d.Name()) {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	})
}
