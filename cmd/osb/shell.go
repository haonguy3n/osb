package main

import (
	"fmt"
	"os"
	"path/filepath"

	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/build"
)

// cmdBinfmt registers QEMU user-mode emulation so foreign-architecture units
// build on this host. Required to build arm64 images on an x86_64 machine (and
// vice versa); native-arch builds never need it.
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

// cmdShell opens an interactive bash shell inside the build container, with the
// same sysroot, PATH, and compiler environment a unit's tasks get. This is the
// tool for debugging a unit whose build fails: reproduce the failing step by
// hand instead of re-running the whole build to read the log.
func cmdShell([]string) {
	projectDir := projectDir()
	sysroot := filepath.Join(projectDir, "build", build.Arch(), "shell", "sysroot")
	build.EnsureDir(sysroot)

	// Use a temp dir for src/destdir so the sandbox mounts are valid
	srcDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "src")
	destDir := filepath.Join(projectDir, "build", build.Arch(), "shell", "destdir")
	build.EnsureDir(srcDir)
	build.EnsureDir(destDir)

	// Same compiler/search-path env the executor gives unit builds
	// (shared definition - see build.SysrootEnv), plus shell context.
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

	// Resolve container image from project
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
