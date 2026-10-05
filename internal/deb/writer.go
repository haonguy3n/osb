package deb

import (
	"time"

	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anhhao17/osb/internal/artifact"
)

func BuildDeb(destDir string, control Control, outputPath, compression string) error {
	debianDir := filepath.Join(destDir, "DEBIAN")
	if err := os.MkdirAll(debianDir, 0755); err != nil {
		return fmt.Errorf("deb: mkdir DEBIAN: %w", err)
	}

	controlPath := filepath.Join(debianDir, "control")
	if _, err := os.Stat(controlPath); err == nil {
	} else {
		if control.InstalledSize == 0 {
			size, err := installedSize(destDir)
			if err != nil {
				return fmt.Errorf("deb: installed size: %w", err)
			}
			control.InstalledSize = size
		}
		f, err := os.Create(controlPath)
		if err != nil {
			return fmt.Errorf("deb: create control: %w", err)
		}
		if err := WriteControl(f, control); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("deb: close control: %w", err)
		}
	}

	if err := writeMd5sums(destDir, debianDir); err != nil {
		return fmt.Errorf("deb: write md5sums: %w", err)
	}

	controlTar, err := buildTarGz(debianDir, nil)
	if err != nil {
		return fmt.Errorf("deb: control.tar.gz: %w", err)
	}
	dataTar, err := buildTarGz(destDir, func(rel string) bool {
		return rel == "DEBIAN" || strings.HasPrefix(rel, "DEBIAN/")
	})
	if err != nil {
		return fmt.Errorf("deb: data.tar.gz: %w", err)
	}

	return writeDebAr(outputPath, []arMember{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "control.tar.gz", data: controlTar},
		{name: "data.tar.gz", data: dataTar},
	})
}

func buildTarGz(root string, skip func(rel string) bool) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if err := tw.WriteHeader(&tar.Header{
		Name:     "./",
		Typeflag: tar.TypeDir,
		Mode:     0755,
		Uname:    "root",
		Gname:    "root",
	}); err != nil {
		return nil, err
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if skip != nil && skip(slashRel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		var link string
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.ModTime = artifact.SourceDateEpoch()
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
		hdr.Name = "./" + slashRel
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid = 0, 0
		hdr.Uname, hdr.Gname = "root", "root"
		hdr.Format = tar.FormatGNU
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type arMember struct {
	name string
	data []byte
}

func writeDebAr(outputPath string, members []arMember) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("deb: create %s: %w", outputPath, err)
	}
	bw := bufio.NewWriter(f)

	if _, err := bw.WriteString("!<arch>\n"); err != nil {
		f.Close()
		return err
	}
	for _, m := range members {
		hdr := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n",
			m.name, 0, 0, 0, "100644", len(m.data))
		if len(hdr) != 60 {
			f.Close()
			return fmt.Errorf("deb: ar header for %q is %d bytes, want 60", m.name, len(hdr))
		}
		if _, err := bw.WriteString(hdr); err != nil {
			f.Close()
			return err
		}
		if _, err := bw.Write(m.data); err != nil {
			f.Close()
			return err
		}
		if len(m.data)%2 == 1 {
			if err := bw.WriteByte('\n'); err != nil {
				f.Close()
				return err
			}
		}
	}

	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func installedSize(destDir string) (int, error) {
	var total int64
	err := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if rel, _ := filepath.Rel(destDir, path); strings.HasPrefix(rel, "DEBIAN") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(total / 1024), nil
}

func writeMd5sums(destDir, debianDir string) error {
	var paths []string
	err := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "DEBIAN/") || rel == "DEBIAN" {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)

	out, err := os.Create(filepath.Join(debianDir, "md5sums"))
	if err != nil {
		return err
	}
	defer out.Close()

	for _, rel := range paths {
		full := filepath.Join(destDir, rel)
		f, err := os.Open(full)
		if err != nil {
			return err
		}
		h := md5.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
		fmt.Fprintf(out, "%x  %s\n", h.Sum(nil), rel)
	}
	return nil
}
