package artifact_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anhhao17/osb/internal/artifact"
	"github.com/anhhao17/osb/internal/repo"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestAPKRoundTripWithUpstreamApk(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	tmp := t.TempDir()
	destDir := filepath.Join(tmp, "destdir")
	if err := os.MkdirAll(filepath.Join(destDir, "usr/bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "usr/bin/hello"),
		[]byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "out")
	unit := &osbstar.Unit{
		Name:        "hello",
		Version:     "1.0.0",
		License:     "MIT",
		Description: "test package for apk compat",
	}
	apkPath, err := artifact.CreateAPK(unit, destDir, "", out, "x86_64", "", nil)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}

	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(apkPath, filepath.Join(work, filepath.Base(apkPath))); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"-v", work+":/work:ro",
		"alpine:3.21",
		"sh", "-c",
		"apk add --allow-untrusted --root /tmp/test --initdb "+
			"/work/"+filepath.Base(apkPath)+" 2>&1; echo EXIT=$?")
	output, _ := cmd.CombinedOutput()
	t.Logf("upstream apk output:\n%s", string(output))

	report := categorizeApkOutput(string(output))
	if report.exitCode != 0 {
		t.Errorf("apk add exited with %d", report.exitCode)
	}
	for _, e := range report.errors {
		t.Errorf("upstream apk ERROR: %s", e)
	}
	for _, w := range report.unexpectedWarnings {
		t.Errorf("upstream apk WARNING: %s", w)
	}
}

func TestAPKRepoInstallWithUpstreamApk(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	tmp := t.TempDir()
	destDir := filepath.Join(tmp, "destdir")
	if err := os.MkdirAll(filepath.Join(destDir, "usr/bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "usr/bin/hello"),
		[]byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(tmp, "repo", "x86_64")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out")
	unit := &osbstar.Unit{
		Name:        "hello",
		Version:     "1.0.0",
		License:     "MIT",
		Description: "test package for apk repo compat",
	}
	apkPath, err := artifact.CreateAPK(unit, destDir, "", out, "x86_64", "", nil)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}
	dst := filepath.Join(repoDir, filepath.Base(apkPath))
	if err := copyFile(apkPath, dst); err != nil {
		t.Fatal(err)
	}

	if err := repo.GenerateIndex(repoDir, nil); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"-v", filepath.Join(tmp, "repo")+":/repo:ro",
		"alpine:3.21",
		"sh", "-c",
		"apk add --allow-untrusted --root /tmp/test --initdb "+
			"--repository /repo --no-network hello 2>&1; echo EXIT=$?")
	output, _ := cmd.CombinedOutput()
	t.Logf("upstream apk output:\n%s", string(output))

	report := categorizeApkOutput(string(output))
	if report.exitCode != 0 {
		t.Errorf("apk add via repo exited with %d", report.exitCode)
	}
	for _, e := range report.errors {
		t.Errorf("upstream apk ERROR: %s", e)
	}
	for _, w := range report.unexpectedWarnings {
		t.Errorf("upstream apk WARNING: %s", w)
	}
}

func TestAPKSignedRepoInstallWithUpstreamApk(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	tmp := t.TempDir()

	keyPath := filepath.Join(tmp, "test-signing.rsa")
	signer, err := artifact.LoadOrGenerateSigner("test", keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateSigner: %v", err)
	}

	destDir := filepath.Join(tmp, "destdir")
	if err := os.MkdirAll(filepath.Join(destDir, "usr/bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "usr/bin/hello"),
		[]byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(tmp, "repo", "x86_64")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "out")
	unit := &osbstar.Unit{
		Name:        "hello",
		Version:     "1.0.0",
		License:     "MIT",
		Description: "test package for signed apk compat",
	}
	apkPath, err := artifact.CreateAPK(unit, destDir, "", out, "x86_64", "", signer)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}
	if err := copyFile(apkPath, filepath.Join(repoDir, filepath.Base(apkPath))); err != nil {
		t.Fatal(err)
	}
	if err := repo.GenerateIndex(repoDir, signer); err != nil {
		t.Fatalf("GenerateIndex: %v", err)
	}

	keysHostDir := filepath.Join(tmp, "keys")
	if err := os.MkdirAll(keysHostDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keysHostDir, signer.KeyName), signer.PubPEM, 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"-v", filepath.Join(tmp, "repo")+":/repo:ro",
		"-v", keysHostDir+":/keys:ro",
		"alpine:3.21",
		"sh", "-c",
		"mkdir -p /tmp/test/etc/apk/keys && "+
			"cp /keys/* /tmp/test/etc/apk/keys/ && "+
			"apk add --root /tmp/test --initdb "+
			"--repository /repo --no-network hello 2>&1; echo EXIT=$?")
	output, _ := cmd.CombinedOutput()
	t.Logf("upstream apk output:\n%s", string(output))

	report := categorizeApkOutput(string(output))
	if report.exitCode != 0 {
		t.Errorf("apk add (signed) exited with %d", report.exitCode)
	}
	for _, e := range report.errors {
		t.Errorf("upstream apk ERROR: %s", e)
	}
	for _, w := range report.expectedWarnings {
		t.Errorf("unexpected (would-be-expected) apk WARNING in signed flow: %s", w)
	}
	for _, w := range report.unexpectedWarnings {
		t.Errorf("upstream apk WARNING: %s", w)
	}
}

type apkReport struct {
	exitCode           int
	errors             []string
	unexpectedWarnings []string
	expectedWarnings   []string
}

func categorizeApkOutput(out string) apkReport {
	r := apkReport{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "EXIT=") {
			fmtSscan(line[5:], &r.exitCode)
			continue
		}
		if strings.HasPrefix(line, "ERROR:") {
			r.errors = append(r.errors, line)
			continue
		}
		if strings.HasPrefix(line, "WARNING:") {
			if isExpectedApkWarning(line) {
				r.expectedWarnings = append(r.expectedWarnings, line)
			} else {
				r.unexpectedWarnings = append(r.unexpectedWarnings, line)
			}
		}
	}
	return r
}

func isExpectedApkWarning(line string) bool {
	return strings.Contains(line, "untrusted") ||
		strings.Contains(line, "no valid signatures")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fmtSscan(s string, v *int) {
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		*v = (*v)*10 + int(c-'0')
	}
}
