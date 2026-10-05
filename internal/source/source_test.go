package source

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestFetchHTTP(t *testing.T) {
	content := createTestTarball(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	t.Setenv("OSB_CACHE", cacheDir)

	unit := &osbstar.Unit{
		Name:   "test-pkg",
		Source: srv.URL + "/test-1.0.tar.gz",
	}

	path, err := Fetch(unit, os.Stdout)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("cached file does not exist: %s", path)
	}

	srv.Close()
	path2, err := Fetch(unit, os.Stdout)
	if err != nil {
		t.Fatalf("second Fetch (cached): %v", err)
	}
	if path != path2 {
		t.Errorf("cached path changed: %s != %s", path, path2)
	}
}

func TestFetchHTTP_SHA256Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("some content"))
	}))
	defer srv.Close()

	t.Setenv("OSB_CACHE", t.TempDir())

	unit := &osbstar.Unit{
		Name:   "bad-hash",
		Source: srv.URL + "/bad.tar.gz",
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	_, err := Fetch(unit, os.Stdout)
	if err == nil {
		t.Fatal("expected SHA256 mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("error should mention SHA256 mismatch: %v", err)
	}
}

func TestPrepare(t *testing.T) {
	content := createTestTarball(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	projectDir := t.TempDir()
	t.Setenv("OSB_CACHE", filepath.Join(projectDir, "cache"))

	unit := &osbstar.Unit{
		Name:    "test-pkg",
		Version: "1.0",
		Source:  srv.URL + "/test-1.0.tar.gz",
	}

	srcDir, err := Prepare(projectDir, "x86_64", "alpine", unit, "", os.Stdout)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	gitDir := filepath.Join(srcDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Fatal("source dir is not a git repo")
	}

	cmd := exec.Command("git", "tag", "-l", "osb/pin")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if !strings.Contains(string(out), "osb/pin") {
		t.Error("osb/pin tag not found")
	}

	if _, err := os.Stat(filepath.Join(srcDir, "hello.txt")); os.IsNotExist(err) {
		t.Error("expected hello.txt in extracted source")
	}
}

func TestPrepare_WithPatches(t *testing.T) {
	content := createTestTarball(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	projectDir := t.TempDir()
	t.Setenv("OSB_CACHE", filepath.Join(projectDir, "cache"))

	patchDir := filepath.Join(projectDir, "test-pkg")
	os.MkdirAll(patchDir, 0755)
	patchContent := `--- a/hello.txt
+++ b/hello.txt
@@ -1 +1 @@
-hello world
+hello patched world
`
	os.WriteFile(filepath.Join(patchDir, "fix.patch"), []byte(patchContent), 0644)

	unit := &osbstar.Unit{
		Name:    "test-pkg",
		Version: "1.0",
		Source:  srv.URL + "/test-1.0.tar.gz",
		Patches: []string{"test-pkg/fix.patch"},
	}

	srcDir, err := Prepare(projectDir, "x86_64", "alpine", unit, "", os.Stdout)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(srcDir, "hello.txt"))
	if err != nil {
		t.Fatalf("reading hello.txt: %v", err)
	}
	if !strings.Contains(string(data), "patched") {
		t.Errorf("patch not applied: content = %q", string(data))
	}

	cmd := exec.Command("git", "rev-list", "--count", "osb/pin..HEAD")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-list: %v", err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		t.Errorf("expected 1 commit beyond osb/pin, got %s", strings.TrimSpace(string(out)))
	}
}

func TestPrepare_PatchesRelativeToDefinedIn(t *testing.T) {
	content := createTestTarball(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	projectDir := t.TempDir()
	t.Setenv("OSB_CACHE", filepath.Join(projectDir, "cache"))

	moduleDir := t.TempDir()
	unitDir := filepath.Join(moduleDir, "units", "bsp")
	patchDir := filepath.Join(unitDir, "test-pkg")
	if err := os.MkdirAll(patchDir, 0755); err != nil {
		t.Fatal(err)
	}
	patchContent := `--- a/hello.txt
+++ b/hello.txt
@@ -1 +1 @@
-hello world
+hello patched world
`
	os.WriteFile(filepath.Join(patchDir, "fix.patch"), []byte(patchContent), 0644)

	unit := &osbstar.Unit{
		Name:      "test-pkg",
		Version:   "1.0",
		Source:    srv.URL + "/test-1.0.tar.gz",
		Patches:   []string{"test-pkg/fix.patch"},
		DefinedIn: unitDir,
	}

	srcDir, err := Prepare(projectDir, "x86_64", "alpine", unit, "", os.Stdout)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(srcDir, "hello.txt"))
	if err != nil {
		t.Fatalf("reading hello.txt: %v", err)
	}
	if !strings.Contains(string(data), "patched") {
		t.Errorf("module-relative patch not applied: content = %q", string(data))
	}
}

func TestPrepare_DevMode(t *testing.T) {
	projectDir := t.TempDir()
	srcDir := filepath.Join(projectDir, "build", "alpine", "test-pkg.x86_64", "src")
	os.MkdirAll(srcDir, 0755)

	run(t, srcDir, "git", "init")
	run(t, srcDir, "git", "config", "user.email", "test@test.com")
	run(t, srcDir, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(srcDir, "main.c"), []byte("int main() {}\n"), 0644)
	run(t, srcDir, "git", "add", "-A")
	run(t, srcDir, "git", "commit", "-m", "upstream")
	run(t, srcDir, "git", "tag", "osb/pin")
	os.WriteFile(filepath.Join(srcDir, "main.c"), []byte("int main() { return 1; }\n"), 0644)
	run(t, srcDir, "git", "add", "-A")
	run(t, srcDir, "git", "commit", "-m", "local change")

	unit := &osbstar.Unit{
		Name:   "test-pkg",
		Source: "https://example.com/should-not-fetch.tar.gz",
	}

	result, err := Prepare(projectDir, "x86_64", "alpine", unit, "", os.Stdout)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result != srcDir {
		t.Errorf("Prepare returned %q, want %q (should reuse local)", result, srcDir)
	}

	data, _ := os.ReadFile(filepath.Join(srcDir, "main.c"))
	if !strings.Contains(string(data), "return 1") {
		t.Error("local changes were overwritten")
	}
}

func TestPrepare_CachedDevSkipsFetch(t *testing.T) {
	projectDir := t.TempDir()
	srcDir := filepath.Join(projectDir, "build", "alpine", "test-pkg.x86_64", "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	run(t, srcDir, "git", "init")
	run(t, srcDir, "git", "config", "user.email", "test@test.com")
	run(t, srcDir, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(srcDir, "main.c"), []byte("int main() {}\n"), 0o644)
	run(t, srcDir, "git", "add", "-A")
	run(t, srcDir, "git", "commit", "-m", "upstream")
	run(t, srcDir, "git", "tag", "osb/pin")
	run(t, srcDir, "git", "remote", "add", "origin", "https://example.com/foo.git")

	unit := &osbstar.Unit{
		Name:   "test-pkg",
		Source: "https://example.com/should-not-fetch.tar.gz",
	}

	var buf bytes.Buffer
	result, err := Prepare(projectDir, "x86_64", "alpine", unit, "dev", &buf)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if result != srcDir {
		t.Errorf("Prepare returned %q, want %q", result, srcDir)
	}
	if !strings.Contains(buf.String(), "Using local source") {
		t.Errorf("expected warning about local source, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "switch back to pin") {
		t.Errorf("warning should hint at pin recovery, got %q", buf.String())
	}
}

func TestPrepare_StaleCacheFallsThrough(t *testing.T) {
	content := createTestTarball(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	projectDir := t.TempDir()
	t.Setenv("OSB_CACHE", filepath.Join(projectDir, "cache"))

	unit := &osbstar.Unit{
		Name:    "test-pkg",
		Version: "1.0",
		Source:  srv.URL + "/test-1.0.tar.gz",
	}

	srcDir, err := Prepare(projectDir, "x86_64", "alpine", unit, "dev", os.Stdout)
	if err != nil {
		t.Fatalf("Prepare with stale dev cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); err != nil {
		t.Errorf("expected fresh clone at %s, got %v", srcDir, err)
	}
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/example/repo.git", true},
		{"git://example.com/repo.git", true},
		{"https://github.com/example/repo", true},
		{"https://example.com/downloads/pkg-1.0.tar.gz", false},
		{"https://github.com/example/repo/archive/v1.0.tar.gz", false},
	}

	for _, tt := range tests {
		got := isGitURL(tt.url)
		if got != tt.want {
			t.Errorf("isGitURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func createTestTarball(t *testing.T) []byte {
	t.Helper()

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "test-1.0")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "Makefile"), []byte("all:\n\techo hello\n"), 0644)

	tarPath := filepath.Join(dir, "test-1.0.tar.gz")
	cmd := exec.Command("tar", "czf", tarPath, "-C", dir, "test-1.0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar: %s\n%s", err, out)
	}

	data, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("reading tarball: %v", err)
	}
	return data
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func TestExtractZipStripsTopLevelDir(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "sample.zip")
	createTestZip(t, zipPath, []zipEntry{
		{name: "tool-1.0/", isDir: true},
		{name: "tool-1.0/bin/", isDir: true},
		{name: "tool-1.0/bin/tool", body: []byte("#!/bin/sh\necho hi\n"), mode: 0o755},
		{name: "tool-1.0/README", body: []byte("docs"), mode: 0o644},
	})

	dest := filepath.Join(tmp, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	tool := filepath.Join(dest, "bin", "tool")
	st, err := os.Stat(tool)
	if err != nil {
		t.Fatalf("expected bin/tool: %v", err)
	}
	if st.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected executable bit on bin/tool, got mode %v", st.Mode())
	}
	body, _ := os.ReadFile(tool)
	if !strings.Contains(string(body), "echo hi") {
		t.Errorf("body mismatch: %q", body)
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Errorf("expected README at top level: %v", err)
	}
}

func TestExtractZipNoCommonPrefix(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "flat.zip")
	createTestZip(t, zipPath, []zipEntry{
		{name: "tool", body: []byte("bin"), mode: 0o755},
		{name: "LICENSE", body: []byte("license"), mode: 0o644},
	})

	dest := filepath.Join(tmp, "out")
	os.MkdirAll(dest, 0o755)
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	for _, name := range []string{"tool", "LICENSE"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Errorf("expected %s at top level: %v", name, err)
		}
	}
}

type zipEntry struct {
	name  string
	body  []byte
	mode  os.FileMode
	isDir bool
}

func TestCopyBareSourceELF(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "kubectl")
	body := append([]byte{0x7f, 'E', 'L', 'F'}, bytes.Repeat([]byte{0}, 60)...)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmp, "out")
	os.MkdirAll(dest, 0o755)

	if err := copyBareSource(src, dest, ""); err != nil {
		t.Fatalf("copyBareSource: %v", err)
	}

	target := filepath.Join(dest, "kubectl")
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected %s: %v", target, err)
	}
	if st.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected executable bit, got %v", st.Mode())
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, body) {
		t.Errorf("bytes mismatch")
	}
}

func createTestZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if !e.isDir {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAPKChecksumVerify(t *testing.T) {
	apkPath := filepath.Join(os.Getenv("HOME"),
		".cache/module-alpine-gen/v3.21/main/x86_64/musl-1.2.5-r11.apk")
	if _, err := os.Stat(apkPath); err != nil {
		t.Skip("test apk not in cache; see comment above to populate it")
	}
	apk, err := os.ReadFile(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(apk)
	}))
	defer srv.Close()

	tmpCache := t.TempDir()
	t.Setenv("OSB_CACHE", tmpCache)

	goodCsum := "Q1KuzxE7sFBvldrt+RbsBErcpFyrM="

	t.Run("matches", func(t *testing.T) {
		u := &osbstar.Unit{
			Name: "musl", Source: srv.URL + "/musl.apk",
			APKChecksum: goodCsum,
		}
		var buf bytes.Buffer
		if _, err := Fetch(u, &buf); err != nil {
			t.Fatalf("fetch failed for valid apk_checksum: %v", err)
		}
	})

	t.Run("mismatch detected", func(t *testing.T) {
		u := &osbstar.Unit{
			Name: "musl", Source: srv.URL + "/musl-bad.apk",
			APKChecksum: "Q1AAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		}
		var buf bytes.Buffer
		_, err := Fetch(u, &buf)
		if err == nil {
			t.Fatal("expected mismatch error, got nil")
		}
		if !strings.Contains(err.Error(), "apk_checksum mismatch") {
			t.Fatalf("expected mismatch error, got: %v", err)
		}
	})

	t.Run("malformed prefix rejected", func(t *testing.T) {
		u := &osbstar.Unit{
			Name: "musl", Source: srv.URL + "/musl-mal.apk",
			APKChecksum: "X1somethingnotsha1=",
		}
		var buf bytes.Buffer
		_, err := Fetch(u, &buf)
		if err == nil || !strings.Contains(err.Error(), "Q1") {
			t.Fatalf("expected Q1 prefix error, got: %v", err)
		}
	})
}
