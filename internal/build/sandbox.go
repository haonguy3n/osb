package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/resolve"
)

type SandboxConfig struct {
	Ctx        context.Context
	Arch       string
	Container  string
	Sandbox    bool
	Shell      string
	BuildRoot  string
	SrcDir     string
	DestDir    string
	Sysroot    string
	Env        map[string]string
	ProjectDir string
	NoUser     bool
	HostDir    string
	CacheDirs  map[string]string
	Stdout     io.Writer
	Stderr     io.Writer
}

func resolveShell(cfg *SandboxConfig) string {
	if cfg.Shell != "" {
		return cfg.Shell
	}
	return "sh"
}

func RunInSandbox(cfg *SandboxConfig, command string) error {
	if !cfg.Sandbox || (cfg.Arch != "" && cfg.Arch != osb.HostArch()) {
		return RunSimple(cfg, command)
	}

	bwrapCmd := bwrapCommand(cfg, command)
	mounts := containerMountsForBuild(cfg)

	return osb.RunInContainer(osb.ContainerRunConfig{
		Ctx:        cfg.Ctx,
		Arch:       cfg.Arch,
		Image:      cfg.Container,
		Command:    bwrapCmd,
		Shell:      resolveShell(cfg),
		ProjectDir: cfg.ProjectDir,
		Mounts:     mounts,
		Stdout:     cfg.Stdout,
		Stderr:     cfg.Stderr,
	})
}

func RunSimple(cfg *SandboxConfig, command string) error {
	var envExports []string
	for k, v := range cfg.Env {
		envExports = append(envExports, fmt.Sprintf("export %s=%q", k, v))
	}
	if cfg.NoUser {
		envExports = append(envExports, `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"`)
	}
	fullCmd := strings.Join(envExports, "; ")
	if fullCmd != "" {
		fullCmd += "; "
	}
	fullCmd += "cd /build/src && " + command

	mounts := containerMountsForBuild(cfg)

	return osb.RunInContainer(osb.ContainerRunConfig{
		Ctx:        cfg.Ctx,
		Arch:       cfg.Arch,
		Image:      cfg.Container,
		Command:    fullCmd,
		Shell:      resolveShell(cfg),
		ProjectDir: cfg.ProjectDir,
		Mounts:     mounts,
		NoUser:     cfg.NoUser,
		Stdout:     cfg.Stdout,
		Stderr:     cfg.Stderr,
	})
}

func bwrapCommand(cfg *SandboxConfig, command string) string {
	var parts []string
	parts = append(parts, "bwrap", "--die-with-parent")

	if cfg.BuildRoot != "" {
		parts = append(parts, "--bind", cfg.BuildRoot, "/")
	} else {
		parts = append(parts, "--bind", "/", "/")
	}

	if cfg.Sysroot != "" {
		parts = append(parts, "--ro-bind", "/build/sysroot", "/build/sysroot")
	}

	parts = append(parts,
		"--bind", "/build/src", "/build/src",
		"--bind", "/build/destdir", "/build/destdir",
		"--dev-bind", "/dev", "/dev",
		"--ro-bind", "/proc", "/proc",
		"--tmpfs", "/tmp",
		"--chdir", "/build/src",
	)

	var envExports []string
	for k, v := range cfg.Env {
		envExports = append(envExports, fmt.Sprintf("export %s=%q", k, v))
	}
	envStr := strings.Join(envExports, "; ")
	fullCmd := envStr
	if fullCmd != "" {
		fullCmd += "; "
	}
	fullCmd += command

	shell := resolveShell(cfg)
	parts = append(parts, "--", shell, "-c", shellQuote(fullCmd))
	return strings.Join(parts, " ")
}

func BwrapShellCommand(cfg *SandboxConfig) string {
	var parts []string
	parts = append(parts, "bwrap", "--die-with-parent")

	if cfg.BuildRoot != "" {
		parts = append(parts, "--bind", cfg.BuildRoot, "/")
	} else {
		parts = append(parts, "--bind", "/", "/")
	}

	if cfg.Sysroot != "" {
		parts = append(parts, "--ro-bind", "/build/sysroot", "/build/sysroot")
	}

	parts = append(parts,
		"--bind", "/build/src", "/build/src",
		"--bind", "/build/destdir", "/build/destdir",
		"--dev-bind", "/dev", "/dev",
		"--ro-bind", "/proc", "/proc",
		"--tmpfs", "/tmp",
		"--chdir", "/build/src",
	)

	shell := resolveShell(cfg)
	var envExports []string
	for k, v := range cfg.Env {
		envExports = append(envExports, fmt.Sprintf("export %s=%q", k, v))
	}
	envStr := strings.Join(envExports, "; ")
	if envStr != "" {
		envStr += "; "
	}
	envStr += "exec " + shell

	parts = append(parts, "--", shell, "-c", shellQuote(envStr))
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func containerMountsForBuild(cfg *SandboxConfig) []osb.Mount {
	var mounts []osb.Mount

	if cfg.SrcDir != "" {
		mounts = append(mounts, osb.Mount{
			Host: cfg.SrcDir, Container: "/build/src",
		})
	}
	if cfg.DestDir != "" {
		mounts = append(mounts, osb.Mount{
			Host: cfg.DestDir, Container: "/build/destdir",
		})
	}
	if cfg.Sysroot != "" {
		mounts = append(mounts, osb.Mount{
			Host: cfg.Sysroot, Container: "/build/sysroot", ReadOnly: true,
		})
	}
	for host, container := range cfg.CacheDirs {
		mounts = append(mounts, osb.Mount{
			Host: host, Container: container,
		})
	}

	return mounts
}

func StageSysroot(destDir, buildDir string) error {
	stageDir := filepath.Join(buildDir, "sysroot-stage")
	os.RemoveAll(stageDir)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("cp", "-al", destDir+"/.", stageDir+"/")
	if err := cmd.Run(); err != nil {
		cmd = exec.Command("cp", "-a", destDir+"/.", stageDir+"/")
		return cmd.Run()
	}
	return nil
}

func AssembleSysroot(sysrootDir string, dag *resolve.DAG, unit string, projectDir string, arch, distro string) error {
	os.RemoveAll(sysrootDir)
	if err := os.MkdirAll(sysrootDir, 0755); err != nil {
		return err
	}
	for _, dep := range dag.TransitiveDeps(unit) {
		stageDir := filepath.Join(UnitBuildDir(projectDir, arch, dep, distro), "sysroot-stage")
		if _, err := os.Stat(stageDir); err != nil {
			continue
		}
		cmd := exec.Command("cp", "-al", stageDir+"/.", sysrootDir+"/")
		if err := cmd.Run(); err != nil {
			cmd = exec.Command("cp", "-a", stageDir+"/.", sysrootDir+"/")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("merging sysroot from %s: %w", dep, err)
			}
		}
	}
	return nil
}

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func NProc() string {
	out, err := exec.Command("nproc").Output()
	if err != nil {
		return "1"
	}
	return strings.TrimSpace(string(out))
}

func multiarchTuple(arch string) string {
	switch arch {
	case "x86_64":
		return "x86_64-linux-gnu"
	case "arm64":
		return "aarch64-linux-gnu"
	case "riscv64":
		return "riscv64-linux-gnu"
	}
	return ""
}

func Arch() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "x86_64"
	}
	arch := strings.TrimSpace(string(out))
	switch arch {
	case "aarch64":
		return "arm64"
	default:
		return arch
	}
}

func UnitBuildDir(projectDir, scopeDir, unitName, distro string) string {
	if distro == "" {
		panic("UnitBuildDir: distro must not be empty (R14a)")
	}
	return filepath.Join(projectDir, "build", distro, unitName+"."+scopeDir)
}
