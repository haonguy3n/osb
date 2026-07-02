package build

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/anhhao17/osb/internal/resolve"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

// GenerateSDK assembles an application-development SDK for an image: the
// union sysroot of the image's resolved closure (headers, libraries and
// pkg-config files, from the sysroot-stage each unit build already
// produces), an environment-setup script, and a docker image pairing the
// sysroot with the ABI-matched toolchain container. Returns the docker
// tag. The image must have been built first — the SDK reuses the staged
// build outputs rather than rebuilding anything.
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

	// Union sysroot of the image's whole closure. AssembleSysroot walks
	// the DAG's transitive deps of the image unit — every artifact plus
	// its build/runtime deps — and merges their staged outputs, exactly
	// as a unit build assembles its private sysroot from its deps.
	sdkDir := filepath.Join(imgBuildDir, "sdk")
	sysroot := filepath.Join(sdkDir, "sysroot")
	fmt.Fprintf(w, "Assembling SDK sysroot from the %s closure...\n", imageName)
	if err := AssembleSysroot(sysroot, dag, imageName, projectDir, arch, distro); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(sysroot, "usr")); err != nil {
		return "", fmt.Errorf("assembled sysroot is empty — build the image first: osb build %s", imageName)
	}

	// Compiler/pkg-config environment, mirroring the executor's build env
	// with the sysroot at its baked-in SDK location. The multiarch paths
	// are inert on alpine and required on the apt distros.
	const sr = "/opt/osb/sysroot"
	tuple := multiarchTuple(arch)
	env := [][2]string{
		{"SYSROOT", sr},
		{"CC", "gcc"},
		{"CXX", "g++"},
		{"CFLAGS", fmt.Sprintf("-I%s/usr/include -I%s/usr/include/%s", sr, sr, tuple)},
		{"CPPFLAGS", fmt.Sprintf("-I%s/usr/include -I%s/usr/include/%s", sr, sr, tuple)},
		{"LDFLAGS", fmt.Sprintf("-L%s/usr/lib -L%s/usr/lib/%s -L%s/lib/%s", sr, sr, tuple, sr, tuple)},
		{"PKG_CONFIG_PATH", fmt.Sprintf("%s/usr/lib/pkgconfig:%s/usr/lib/%s/pkgconfig:%s/usr/share/pkgconfig", sr, sr, tuple, sr)},
		{"LD_LIBRARY_PATH", fmt.Sprintf("%s/usr/lib:%s/usr/lib/%s:%s/lib/%s", sr, sr, tuple, sr, tuple)},
	}

	// environment-setup: the same env as the baked ENV lines, for anyone
	// consuming the sysroot outside the docker image (CI tar, IDE).
	setup := "# osb SDK environment — source this when using the sysroot outside\n" +
		"# the SDK container (adjust SYSROOT to where you unpacked it).\n"
	for _, kv := range env {
		setup += fmt.Sprintf("export %s=\"%s\"\n", kv[0], kv[1])
	}
	setup += fmt.Sprintf("export PATH=\"%s/usr/bin:$PATH\"\n", sr)
	if err := os.WriteFile(filepath.Join(sdkDir, "environment-setup"), []byte(setup), 0755); err != nil {
		return "", err
	}

	// Bake the SDK docker image on top of the toolchain the image's
	// closure builds with (distro-dispatched: musl for alpine, glibc for
	// debian/ubuntu), so compiler and libc match the target ABI.
	toolchain := resolveContainerImage(proj, unit, arch, distro)
	if toolchain == "" {
		return "", fmt.Errorf("image %q has no toolchain container to base the SDK on", imageName)
	}
	dockerfile := fmt.Sprintf("FROM %s\nCOPY sysroot %s\nCOPY environment-setup /opt/osb/environment-setup\n", toolchain, sr)
	for _, kv := range env {
		dockerfile += fmt.Sprintf("ENV %s=\"%s\"\n", kv[0], kv[1])
	}
	dockerfile += fmt.Sprintf("ENV PATH=\"%s/usr/bin:$PATH\"\n", sr)
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
