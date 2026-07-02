package build

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/anhhao17/osb/internal/resolve"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

// GenerateSDK assembles an app-dev SDK for a built image — the union
// sysroot of its closure baked into a docker image on the ABI-matched
// toolchain — and returns the docker tag. See docs/build-environment.md.
func GenerateSDK(w io.Writer, proj *osbstar.Project, projectDir, imageName, arch, machine string) (string, error) {
	distro, err := proj.EffectiveDistroForImage(imageName)
	if err != nil {
		return "", err
	}
	unit := proj.LookupUnit(distro, imageName)
	if unit == nil {
		unit = proj.AnyUnit(imageName)
	}
	if unit == nil {
		return "", fmt.Errorf("image %q not found", imageName)
	}
	if unit.Class != "image" {
		return "", fmt.Errorf("%q is a %s, not an image — the SDK is generated from an image's closure", imageName, unit.Class)
	}

	scope := ScopeDir(unit, arch, machine)
	imgBuildDir := UnitBuildDir(projectDir, scope, imageName, distro)
	if _, err := os.Stat(imgBuildDir); err != nil {
		return "", fmt.Errorf("image %q has no build output yet — run: osb build %s", imageName, imageName)
	}

	dag, err := resolve.BuildDAG(proj, distro)
	if err != nil {
		return "", err
	}

	sdkDir := filepath.Join(imgBuildDir, "sdk")
	sysroot := filepath.Join(sdkDir, "sysroot")
	fmt.Fprintf(w, "Assembling SDK sysroot from the %s closure...\n", imageName)
	if err := AssembleSysroot(sysroot, dag, imageName, projectDir, arch, distro); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(sysroot, "usr")); err != nil {
		return "", fmt.Errorf("assembled sysroot is empty — build the image first: osb build %s", imageName)
	}

	// Keys sorted for deterministic Dockerfile/environment-setup output.
	const sr = "/opt/osb/sysroot"
	env := SysrootEnv(sr, arch)
	env["SYSROOT"] = sr
	env["CC"] = "gcc"
	env["CXX"] = "g++"
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	setup := "# osb SDK environment — source this when using the sysroot outside\n" +
		"# the SDK container (adjust SYSROOT to where you unpacked it).\n"
	for _, k := range keys {
		setup += fmt.Sprintf("export %s=\"%s\"\n", k, env[k])
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "environment-setup"), []byte(setup), 0755); err != nil {
		return "", err
	}

	toolchain := resolveContainerImage(proj, unit, arch, distro)
	if toolchain == "" {
		return "", fmt.Errorf("image %q has no toolchain container to base the SDK on", imageName)
	}
	dockerfile := fmt.Sprintf("FROM %s\nCOPY sysroot %s\nCOPY environment-setup /opt/osb/environment-setup\n", toolchain, sr)
	for _, k := range keys {
		dockerfile += fmt.Sprintf("ENV %s=\"%s\"\n", k, env[k])
	}
	dockerfile += "WORKDIR /work\n"
	if err := os.WriteFile(filepath.Join(sdkDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return "", err
	}

	tag := fmt.Sprintf("osb/sdk-%s-%s:%s-%s", proj.Name, imageName, distro, arch)
	fmt.Fprintf(w, "Baking SDK image %s (from %s)...\n", tag, toolchain)
	cmd := exec.Command("docker", "build", "-t", tag, sdkDir)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build: %w", err)
	}
	return tag, nil
}

// RunSDKShell opens an interactive shell in a generated SDK image with
// the caller's working directory mounted at /work.
func RunSDKShell(tag, workDir string) error {
	cmd := exec.Command("docker", "run", "--rm", "-it",
		"-v", workDir+":/work", "-w", "/work", tag, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
