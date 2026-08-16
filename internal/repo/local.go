package repo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/anhhao17/osb/internal/artifact"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

// RepoDir returns the local package repository path for a project.
// Repos are scoped per project: repo/<project-name>/. This is the
// project-wide base that the feed server serves under /<project>/;
// per-distro emitters and consumers should use RepoDistroDir instead.
// This prevents stale packages from one project contaminating another's APKINDEX.
func RepoDir(proj *osbstar.Project, projectDir string) string {
	if proj != nil && proj.Name != "" {
		return filepath.Join(projectDir, "repo", proj.Name)
	}
	return filepath.Join(projectDir, "repo")
}

// RepoDistroDir returns the per-distro subtree under a project's repo:
// repo/<project>/<distro>/. APK emitters publish per-arch APKINDEX
// trees under repo/<project>/alpine/<arch>/; Debian emitters publish
// dists/<suite>/InRelease + pool/... under repo/<project>/debian/.
// Splitting at the distro level lets a single project hold both kinds
// of feed without name collisions and lets device-side apk + apt
// reference unambiguous URLs (e.g. /<proj>/alpine/<arch>,
// /<proj>/debian).
//
// An empty distro is a programmer error and panics - every emitter
// and image-assembly consumer knows which backend it's targeting.
func RepoDistroDir(proj *osbstar.Project, projectDir, distro string) string {
	if distro == "" {
		panic("RepoDistroDir: distro must not be empty (R14)")
	}
	return filepath.Join(RepoDir(proj, projectDir), distro)
}

// Publish copies an .apk file into the per-arch subdirectory of the local
// repository and regenerates the APKINDEX for that arch. The on-disk layout
// matches Alpine's convention so `apk add -X <repoDir>` Just Works:
//
//	<repoDir>/<archDir>/<pkg>-<ver>-r<N>.apk
//	<repoDir>/<archDir>/APKINDEX.tar.gz
//	<repoDir>/keys/<keyname>.rsa.pub  (when signer is non-nil)
//
// archDir is the package's arch ("x86_64", "aarch64", ...) or the literal
// "noarch" for portable packages. The public key sits in <repoDir>/keys/
// so apk add can verify the repo via `--keys-dir <repoDir>/keys` without
// any further configuration.
func Publish(apkPath, repoDir, archDir string, signer *artifact.Signer) error {
	archPath := filepath.Join(repoDir, archDir)
	if err := os.MkdirAll(archPath, 0755); err != nil {
		return err
	}

	name := filepath.Base(apkPath)
	dst := filepath.Join(archPath, name)

	src, err := os.Open(apkPath)
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return err
	}

	if signer != nil {
		if err := WritePublicKey(repoDir, signer); err != nil {
			return fmt.Errorf("publishing public key: %w", err)
		}
	}

	if err := GenerateIndex(archPath, signer); err != nil {
		return err
	}
	// A noarch publish has to refresh every per-arch APKINDEX too -
	// each one includes noarch entries via GenerateIndex's sibling
	// scan, so adding a noarch apk silently invalidates them all.
	if archDir == "noarch" {
		archDirs, err := ArchDirs(repoDir)
		if err != nil {
			return err
		}
		for _, ad := range archDirs {
			if ad == "noarch" {
				continue
			}
			if err := GenerateIndex(filepath.Join(repoDir, ad), signer); err != nil {
				return fmt.Errorf("regenerating %s APKINDEX after noarch publish: %w", ad, err)
			}
		}
	}
	return nil
}

// WritePublicKey drops the project's public key into <repoDir>/keys/ so
// image-time apk add can find it via --keys-dir, and so anyone consuming
// the repo can install signature verification with a single file copy.
func WritePublicKey(repoDir string, signer *artifact.Signer) error {
	keysDir := filepath.Join(repoDir, "keys")
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(keysDir, signer.KeyName), signer.PubPEM, 0644)
}

// ArchDirs returns the per-arch subdirectories that hold .apk files in repoDir.
// Useful for callers that need to walk every arch's contents (list, info,
// remove, and the image-rootfs assembler that searches multiple arch dirs).
func ArchDirs(repoDir string) ([]string, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
