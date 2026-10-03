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

func RepoDir(proj *osbstar.Project, projectDir string) string {
	if proj != nil && proj.Name != "" {
		return filepath.Join(projectDir, "repo", proj.Name)
	}
	return filepath.Join(projectDir, "repo")
}

func RepoDistroDir(proj *osbstar.Project, projectDir, distro string) string {
	if distro == "" {
		panic("RepoDistroDir: distro must not be empty (R14)")
	}
	return filepath.Join(RepoDir(proj, projectDir), distro)
}

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

func WritePublicKey(repoDir string, signer *artifact.Signer) error {
	keysDir := filepath.Join(repoDir, "keys")
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(keysDir, signer.KeyName), signer.PubPEM, 0644)
}

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
