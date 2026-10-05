package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func RunClean(projectDir string, all bool, units []string) error {
	buildDir := filepath.Join(projectDir, "build")
	if len(units) == 0 {
		dirs := []string{buildDir}
		if all {
			dirs = append(dirs, filepath.Join(projectDir, "repo"))
		}
		for _, dir := range dirs {
			if err := RemoveDirAnyOwner(dir, projectDir); err != nil {
				return fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		fmt.Printf("Removed %s\n", strings.Join(dirs, ", "))
		return nil
	}
	for _, u := range units {
		matches, err := filepath.Glob(filepath.Join(buildDir, "*", u+".*"))
		if err != nil {
			return err
		}
		for _, dir := range matches {
			if err := RemoveDirAnyOwner(dir, projectDir); err != nil {
				return fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		fmt.Printf("Cleaned %s (%d build dirs)\n", u, len(matches))
	}
	return nil
}

func RemoveDirAnyOwner(dir, projectDir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return nil
	}
	rel, err := filepath.Rel(projectDir, dir)
	if err != nil {
		return fmt.Errorf("computing container path for %s: %w", dir, err)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to container-rm a path outside the project tree: %s", dir)
	}
	cPath := "/project/" + filepath.ToSlash(rel)
	image := LocalToolchainImage(HostArch())
	if image == "" {
		return fmt.Errorf("cannot remove root-owned files in %s: no local osb toolchain image found to run container-side rm "+
			"(build a target first, or remove the directory manually with sudo)", dir)
	}
	return RunInContainer(ContainerRunConfig{
		Image:      image,
		Command:    "rm -rf " + cPath,
		ProjectDir: projectDir,
		NoUser:     true,
		Quiet:      true,
	})
}
