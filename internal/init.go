package internal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func RunInit(projectDir, machine, distro string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "PROJECT.star")); err == nil {
		return fmt.Errorf("project already exists at %s (PROJECT.star found)", projectDir)
	}
	if machine == "" {
		machine = "qemu-x86_64"
	}
	if distro == "" {
		distro = "alpine"
	}
	replacer := strings.NewReplacer(
		"{{NAME}}", filepath.Base(projectDir),
		"{{MACHINE}}", machine,
		"{{DISTRO}}", distro,
	)
	for _, dir := range []string{"machines", "units", "images", "classes"} {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
			return err
		}
	}
	err := fs.WalkDir(skeletonFS, "skeleton", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, "skeleton/")
		if rel == "gitignore" {
			rel = ".gitignore"
		}
		data, err := skeletonFS.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(projectDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(replacer.Replace(string(data))), 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Printf("Created osb project at %s\n", projectDir)
	return nil
}
