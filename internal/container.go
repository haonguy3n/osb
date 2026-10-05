package internal

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func HostArch() string {
	return hostArch()
}

func hostArch() string {
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

type Mount struct {
	Host      string
	Container string
	ReadOnly  bool
}

type ContainerRunConfig struct {
	Shell       string
	Ctx         context.Context
	Arch        string
	Image       string
	Command     string
	ProjectDir  string
	Mounts      []Mount
	Env         map[string]string
	Interactive bool
	NoUser      bool
	Stdout      io.Writer
	Stderr      io.Writer
	Quiet       bool
}

var OnNotify func(string)

func DefaultContainerImage(proj *osbstar.Project) string {
	arch := HostArch()
	if proj != nil {
		if cu := proj.AnyUnit("toolchain-musl"); cu != nil {
			return fmt.Sprintf("osb/toolchain-musl:%s-%s", cu.Version, arch)
		}
	}
	return fmt.Sprintf("osb/toolchain-musl:15-%s", arch)
}

func LocalToolchainImage(arch string) string {
	runtime, err := detectRuntime()
	if err != nil {
		return ""
	}
	out, err := exec.Command(runtime, "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return ""
	}
	suffix := "-" + arch
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "osb/toolchain") && strings.HasSuffix(line, suffix) {
			return line
		}
	}
	return ""
}

func RunInContainer(cfg ContainerRunConfig) error {
	if cfg.Image == "" {
		return fmt.Errorf("no container image specified")
	}

	runtime, err := detectRuntime()
	if err != nil {
		return err
	}

	args, err := containerRunArgs(cfg)
	if err != nil {
		return err
	}

	name := fmt.Sprintf("osb-%d", rand.Int())
	args = append(args[:1], append([]string{"--name", name}, args[1:]...)...)

	args = append(args, cfg.Command)

	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if !cfg.Quiet {
		fmt.Fprintf(stderr, "[osb] container: %s\n", cfg.Command)
	}

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	done := make(chan struct{})
	if ctx != context.Background() {
		go func() {
			select {
			case <-ctx.Done():
				//nolint:gosec // best-effort cleanup
				exec.Command(runtime, "stop", "-t", "3", name).Run()
			case <-done:
			}
		}()
	}

	cmd := exec.Command(runtime, args...)
	cmd.Stdout = cfg.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	watcher := &usernsWatcher{w: stderr}
	cmd.Stderr = watcher
	if cfg.Interactive {
		cmd.Stdin = os.Stdin
	}

	err = cmd.Run()
	close(done)

	if ctx.Err() != nil {
		return fmt.Errorf("build cancelled")
	}
	if err != nil && watcher.tripped {
		return usernsError(err)
	}
	return err
}

func containerRunArgs(cfg ContainerRunConfig) ([]string, error) {
	arch := cfg.Arch
	if arch == "" {
		arch = hostArch()
	}

	args := []string{"run", "--rm", "--privileged"}

	if strings.HasPrefix(cfg.Image, "osb/") {
		args = append(args, "--pull=never")
	}

	args = append(args, "--platform", "linux/"+arch)

	if !cfg.NoUser {
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("getting current user: %w", err)
		}
		args = append(args, "--user", fmt.Sprintf("%s:%s", u.Uid, u.Gid))
	}

	if cfg.ProjectDir != "" {
		args = append(args, "-v", cfg.ProjectDir+":/project")
	}

	for _, m := range cfg.Mounts {
		mount := m.Host + ":" + m.Container
		if m.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	for k, v := range cfg.Env {
		args = append(args, "-e", k+"="+v)
	}

	if cfg.Interactive {
		args = append(args, "-it")
	}

	args = append(args, "-w", "/project")
	args = append(args, cfg.Image)
	shell := cfg.Shell
	if shell == "" {
		shell = "sh"
	}
	args = append(args, shell, "-c")

	return args, nil
}

func RegisterBinfmt(w io.Writer) error {
	runtime, err := detectRuntime()
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "[osb] registering binfmt_misc handlers...")
	cmd := exec.Command(runtime, "run", "--privileged", "--rm",
		"tonistiigi/binfmt", "--install", "arm64,riscv64")
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("registering binfmt: %w", err)
	}

	fmt.Fprintln(w, "Done. Registered: arm64, riscv64")
	return nil
}

func detectRuntime() (string, error) {
	for _, rt := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(rt); err == nil {
			return rt, nil
		}
	}
	return "", fmt.Errorf("neither docker nor podman found - install one to use osb")
}
