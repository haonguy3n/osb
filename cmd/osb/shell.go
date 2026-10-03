package main

import (
	"fmt"
	"os"
	"path/filepath"

	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/build"
)

func cmdBinfmt([]string) {
	fmt.Println("This will register QEMU user-mode emulation for foreign architectures")
	fmt.Println("by running a privileged Docker container (tonistiigi/binfmt).")
	fmt.Println()
	fmt.Println("This enables building arm64 and riscv64 images on your " + build.Arch() + " host.")
	fmt.Println("The registration persists until reboot.")
	fmt.Println()
	fmt.Print("Proceed? (y/n) ")
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return
	}
	if err := osb.RegisterBinfmt(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdShell([]string) {
	projectDir := projectDir()
	sysroot := filepath.Join(projectDir, "build", build.Arch(), "shell", "sysroot")
	build.EnsureDir(sysroot)

	srcDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "src")
	destDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "destdir")
	build.EnsureDir(srcDir)
	build.EnsureDir(destDir)

	shellEnv := build.SysrootEnv("/build/sysroot", build.Arch())
	shellEnv["PREFIX"] = "/usr"
	shellEnv["DESTDIR"] = "/build/destdir"
	shellEnv["NPROC"] = build.NProc()
	shellEnv["ARCH"] = build.Arch()
	shellEnv["HOME"] = "/tmp"

	cfg := &build.SandboxConfig{
		Sandbox:    true,
		Shell:      "bash",
		SrcDir:     srcDir,
		DestDir:    destDir,
		Sysroot:    sysroot,
		ProjectDir: projectDir,
		Env:        shellEnv,
	}

	bwrapCmd := build.BwrapShellCommand(cfg)
	mounts := []osb.Mount{
		{Host: srcDir, Container: "/build/src"},
		{Host: destDir, Container: "/build/destdir"},
		{Host: sysroot, Container: "/build/sysroot", ReadOnly: true},
	}

	proj := loadProject()

	if err := osb.RunInContainer(osb.ContainerRunConfig{
		Shell:       "bash",
		Image:       osb.DefaultContainerImage(proj),
		Command:     bwrapCmd,
		ProjectDir:  projectDir,
		Mounts:      mounts,
		Interactive: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
