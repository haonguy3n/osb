package source

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func Prepare(projectDir, scopeDir, distro string, unit *osbstar.Unit, cachedSourceState string, w io.Writer) (string, error) {
	if distro == "" {
		return "", fmt.Errorf("source.Prepare: distro must not be empty (unit %q)", unit.Name)
	}
	srcDir := filepath.Join(projectDir, "build", distro, unit.Name+"."+scopeDir, "src")

	if IsDev(State(cachedSourceState)) {
		if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
			fmt.Fprintf(w, "Using local source for %s (state %s) - "+
				".star source/tag/patches changes won't apply until you switch back to pin\n",
				unit.Name, cachedSourceState)
			return srcDir, nil
		}
	}

	if cachedSourceState == string(StatePin) && unit.Tag != "" {
		if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
			if upstreamMatchesTag(srcDir, unit.Tag) {
				fmt.Fprintf(w, "Using existing pin checkout for %s (upstream at %s)\n", unit.Name, unit.Tag)
				return srcDir, nil
			}
		}
	}

	if hasLocalCommits(srcDir) {
		fmt.Fprintf(w, "Using local source for %s (has commits beyond upstream)\n", unit.Name)
		return srcDir, nil
	}

	if unit.Source == "" {
		return "", fmt.Errorf("unit %q has no source", unit.Name)
	}

	if local := LocalDir(unit); local != "" {
		makeRemovable(srcDir)
		if err := os.RemoveAll(srcDir); err != nil {
			return "", err
		}
		if err := copyLocalDir(local, srcDir); err != nil {
			return "", fmt.Errorf("copying local source %s: %w", local, err)
		}
		if err := initGitRepo(srcDir); err != nil {
			return "", err
		}
		return srcDir, applyPatches(projectDir, srcDir, unit)
	}

	cachedPath, err := Fetch(unit, w)
	if err != nil {
		return "", err
	}

	makeRemovable(srcDir)
	if err := os.RemoveAll(srcDir); err != nil {
		return "", fmt.Errorf("removing existing %s: %w (file may be owned by a different user - try `sudo rm -rf %s`)", srcDir, err, srcDir)
	}
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return "", err
	}

	if isGitURL(unit.Source) {
		if err := checkoutGit(cachedPath, srcDir, unit); err != nil {
			return "", err
		}
		if err := tagUpstream(srcDir); err != nil {
			return "", err
		}
	} else {
		if err := prepareNonGitSource(cachedPath, srcDir, unit.Source); err != nil {
			return "", err
		}
		if err := initGitRepo(srcDir); err != nil {
			return "", err
		}
	}

	if err := applyPatches(projectDir, srcDir, unit); err != nil {
		return "", err
	}

	return srcDir, nil
}

func upstreamMatchesTag(srcDir, tag string) bool {
	upstream, err := exec.Command("git", "-C", srcDir, "rev-parse", PinTag+"^{commit}").Output()
	if err != nil {
		return false
	}
	pin, err := exec.Command("git", "-C", srcDir, "rev-parse", tag+"^{commit}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(upstream)) == strings.TrimSpace(string(pin))
}

func makeRemovable(dir string) {
	if _, err := os.Stat(dir); err != nil {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		_ = os.Chmod(path, 0o700)
		return nil
	})
}

func hasLocalCommits(srcDir string) bool {
	gitDir := filepath.Join(srcDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return false
	}

	cmd := exec.Command("git", "rev-list", "--count", PinTag+"..HEAD")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	count := strings.TrimSpace(string(out))
	return count != "0"
}

func checkoutGit(barePath, srcDir string, unit *osbstar.Unit) error {
	ref := "HEAD"
	if unit.Tag != "" {
		ref = unit.Tag
	} else if unit.Branch != "" {
		ref = unit.Branch
	}

	cmd := exec.Command("git", "clone", "--shared", barePath, srcDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %s\n%s", err, out)
	}

	cmd = exec.Command("git", "checkout", ref)
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %s\n%s", ref, err, out)
	}

	return nil
}

func prepareNonGitSource(cachedPath, destDir, sourceURL string) error {
	switch {
	case strings.HasSuffix(cachedPath, ".tar.gz"),
		strings.HasSuffix(cachedPath, ".tgz"),
		strings.HasSuffix(cachedPath, ".tar.xz"),
		strings.HasSuffix(cachedPath, ".tar.bz2"),
		strings.HasSuffix(cachedPath, ".tbz2"),
		strings.HasSuffix(cachedPath, ".tar"):
		return extractTarball(cachedPath, destDir)
	case strings.HasSuffix(cachedPath, ".zip"):
		return extractZip(cachedPath, destDir)
	case strings.HasSuffix(cachedPath, ".apk"):
		return copyBareSource(cachedPath, destDir, urlBasename(sourceURL))
	case strings.HasSuffix(cachedPath, ".deb"):
		return copyBareSource(cachedPath, destDir, urlBasename(sourceURL))
	}

	f, err := os.Open(cachedPath)
	if err != nil {
		return err
	}
	var magic [4]byte
	n, _ := io.ReadFull(f, magic[:])
	f.Close()

	if n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return extractTarball(cachedPath, destDir)
	}
	if n == 4 && magic[0] == 0x50 && magic[1] == 0x4b && magic[2] == 0x03 && magic[3] == 0x04 {
		return extractZip(cachedPath, destDir)
	}
	return copyBareSource(cachedPath, destDir, urlBasename(sourceURL))
}

func urlBasename(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.LastIndexByte(rawURL, '/'); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func extractTarball(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f

	switch {
	case strings.HasSuffix(tarPath, ".gz") || strings.HasSuffix(tarPath, ".tgz"):
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	case strings.HasSuffix(tarPath, ".bz2"):
		reader = bzip2.NewReader(f)
	case strings.HasSuffix(tarPath, ".xz"):
		return extractWithTar(tarPath, destDir)
	}

	tr := tar.NewReader(reader)
	stripPrefix := ""

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tarball: %w", err)
		}

		if stripPrefix == "" {
			parts := strings.SplitN(hdr.Name, "/", 2)
			if len(parts) > 1 {
				stripPrefix = parts[0] + "/"
			}
		}

		name := strings.TrimPrefix(hdr.Name, stripPrefix)
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destDir, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(hdr.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0755)
			os.Symlink(hdr.Linkname, target)
		}
	}

	return nil
}

func extractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer zr.Close()

	stripPrefix := ""
	for i, f := range zr.File {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			stripPrefix = ""
			break
		}
		first := parts[0] + "/"
		if i == 0 {
			stripPrefix = first
			continue
		}
		if first != stripPrefix {
			stripPrefix = ""
			break
		}
	}

	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, stripPrefix)
		if name == "" || name == "." {
			continue
		}
		target := filepath.Join(destDir, name)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()|0o700); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := f.Mode()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

func copyBareSource(filePath, destDir, targetName string) error {
	if targetName == "" {
		targetName = filepath.Base(filePath)
	}
	target := filepath.Join(destDir, targetName)

	in, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(target, 0o755)
}

func extractWithTar(tarPath, destDir string) error {
	cmd := exec.Command("tar", "xf", tarPath, "--strip-components=1", "-C", destDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar extract: %s\n%s", err, out)
	}
	return nil
}

func tagUpstream(srcDir string) error {
	branchCmd := exec.Command("git", "checkout", "-b", "osb-work")
	branchCmd.Dir = srcDir
	branchCmd.Run()
	cmd := exec.Command("git", "tag", "-f", PinTag)
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git tag %s: %s\n%s", PinTag, err, out)
	}
	return nil
}

func gitCommitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=osb",
		"GIT_AUTHOR_EMAIL=osb@osb.local",
		"GIT_COMMITTER_NAME=osb",
		"GIT_COMMITTER_EMAIL=osb@osb.local",
	)
}

func initGitRepo(srcDir string) error {
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "osb@osb.local"},
		{"git", "config", "user.name", "osb"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "upstream source"},
		{"git", "tag", PinTag},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = srcDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s\n%s", strings.Join(args, " "), err, out)
		}
	}

	return nil
}

func applyPatches(projectDir, srcDir string, unit *osbstar.Unit) error {
	baseDir := unit.DefinedIn
	if baseDir == "" {
		baseDir = projectDir
	}
	for _, patchFile := range unit.Patches {
		patchPath := filepath.Join(baseDir, patchFile)
		if _, err := os.Stat(patchPath); os.IsNotExist(err) {
			return fmt.Errorf("patch file not found: %s", patchFile)
		}
		if abs, err := filepath.Abs(patchPath); err == nil {
			patchPath = abs
		}

		cmd := exec.Command("git", "am", "--3way", patchPath)
		cmd.Dir = srcDir
		cmd.Env = gitCommitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			cmd = exec.Command("git", "apply", patchPath)
			cmd.Dir = srcDir
			if out2, err2 := cmd.CombinedOutput(); err2 != nil {
				return fmt.Errorf("applying %s: git am: %s\ngit apply: %s\n%s\n%s",
					patchFile, err, err2, out, out2)
			}
			commitMsg := fmt.Sprintf("patch: %s", filepath.Base(patchFile))
			cmds := [][]string{
				{"git", "add", "-A"},
				{"git", "commit", "-m", commitMsg},
			}
			for _, args := range cmds {
				c := exec.Command(args[0], args[1:]...)
				c.Dir = srcDir
				c.Env = gitCommitEnv()
				if out, err := c.CombinedOutput(); err != nil {
					return fmt.Errorf("%s: %s\n%s", strings.Join(args, " "), err, out)
				}
			}
		}
	}

	return nil
}
