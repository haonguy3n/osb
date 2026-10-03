package deb

import (
	"fmt"
	"os"
	"path/filepath"
)

func MaterializeSystemdServiceSymlinks(destDir, sysroot string, services []string) error {
	if len(services) == 0 {
		return nil
	}
	wantsDir := filepath.Join(destDir, "etc", "systemd", "system", "multi-user.target.wants")
	for _, svc := range services {
		unitFile := svc + ".service"
		if !serviceFileAvailable(destDir, sysroot, unitFile) {
			return fmt.Errorf("service %q declared but /lib/systemd/system/%s missing in destdir or sysroot", svc, unitFile)
		}
		linkPath := filepath.Join(wantsDir, unitFile)
		if _, err := os.Lstat(linkPath); err == nil {
			continue
		}
		if err := os.MkdirAll(wantsDir, 0755); err != nil {
			return err
		}
		if err := os.Symlink("/lib/systemd/system/"+unitFile, linkPath); err != nil {
			return err
		}
	}
	return nil
}

func serviceFileAvailable(destDir, sysroot, unitFile string) bool {
	candidates := []string{
		filepath.Join("lib", "systemd", "system", unitFile),
		filepath.Join("usr", "lib", "systemd", "system", unitFile),
		filepath.Join("etc", "systemd", "system", unitFile),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(destDir, c)); err == nil {
			return true
		}
	}
	if sysroot == "" {
		return false
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(sysroot, c)); err == nil {
			return true
		}
	}
	return false
}
