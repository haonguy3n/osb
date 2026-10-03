package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func RunClean(projectDir, _ string, all bool, force bool, units []string) error {
	buildDir := filepath.Join(projectDir, "build")

	if len(units) > 0 {
		for _, r := range units {
			matches, err := filepath.Glob(filepath.Join(buildDir, "*", r+".*"))
			if err != nil {
				return fmt.Errorf("globbing %s: %w", r, err)
			}
			for _, dir := range matches {
				if err := RemoveDirAnyOwner(dir, projectDir); err != nil {
					return fmt.Errorf("removing %s: %w", dir, err)
				}
			}
			if len(matches) == 0 {
				fmt.Printf("Cleaned %s (no on-disk build dirs)\n", r)
			} else {
				fmt.Printf("Cleaned %s (%d build dirs)\n", r, len(matches))
			}
		}
		return nil
	}

	if all {
		if !force {
			fmt.Print("Remove all build artifacts and packages? [y/N] ")
			if !confirmYes() {
				fmt.Println("Aborted")
				return nil
			}
		}
		dirs := []string{buildDir, filepath.Join(projectDir, "repo")}
		for _, dir := range dirs {
			if err := RemoveDirAnyOwner(dir, projectDir); err != nil {
				return fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		fmt.Println("Cleaned all build artifacts, packages, and sources")
	} else {
		if !force {
			fmt.Print("Remove all build intermediates? [y/N] ")
			if !confirmYes() {
				fmt.Println("Aborted")
				return nil
			}
		}
		if err := RemoveDirAnyOwner(buildDir, projectDir); err != nil {
			return fmt.Errorf("removing %s: %w", buildDir, err)
		}
		fmt.Println("Cleaned build intermediates (packages preserved)")
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

func CleanLocks(projectDir, _ string) error {
	lockPaths, err := filepath.Glob(filepath.Join(projectDir, "build", "*", "*.*/.lock"))
	if err != nil {
		return err
	}
	if len(lockPaths) == 0 {
		if _, err := os.Stat(filepath.Join(projectDir, "build")); os.IsNotExist(err) {
			fmt.Println("No build directory")
			return nil
		}
		fmt.Println("No stale locks found")
		return nil
	}
	for _, lockPath := range lockPaths {
		os.Remove(lockPath)
		rel, _ := filepath.Rel(filepath.Join(projectDir, "build"), filepath.Dir(lockPath))
		fmt.Printf("Removed lock: %s\n", rel)
	}
	fmt.Printf("Removed %d lock(s)\n", len(lockPaths))
	return nil
}

func confirmYes() bool {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(scanner.Text()), "y")
}
