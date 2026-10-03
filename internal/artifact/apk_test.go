package artifact

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestCreateAPK(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "destdir")
	os.MkdirAll(filepath.Join(destDir, "usr", "bin"), 0755)
	os.MkdirAll(filepath.Join(destDir, "etc"), 0755)
	os.WriteFile(filepath.Join(destDir, "usr", "bin", "hello"), []byte("#!/bin/sh\necho hello\n"), 0755)
	os.WriteFile(filepath.Join(destDir, "etc", "hello.conf"), []byte("key=value\n"), 0644)

	outputDir := filepath.Join(t.TempDir(), "output")

	unit := &osbstar.Unit{
		Name:        "hello",
		Version:     "1.0.0",
		Description: "Hello world",
		License:     "MIT",
		RuntimeDeps: []string{"glibc"},
	}

	apkPath, err := CreateAPK(unit, destDir, "", outputDir, "x86_64", "", nil)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}

	if _, err := os.Stat(apkPath); os.IsNotExist(err) {
		t.Fatal("apk file not created")
	}

	f, err := os.Open(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var files []string
	hasPKGINFO := false
	var pkginfoContent string

	br := bufio.NewReader(f)
	for {
		gr, err := gzip.NewReader(br)
		if err != nil {
			break
		}
		gr.Multistream(false)

		tr := tar.NewReader(gr)
		for {
			hdr, err := tr.Next()
			if err != nil {
				break
			}
			files = append(files, hdr.Name)
			if hdr.Name == ".PKGINFO" {
				hasPKGINFO = true
				data, _ := io.ReadAll(tr)
				pkginfoContent = string(data)
			}
		}
		io.Copy(io.Discard, gr)
		gr.Close()
	}

	if !hasPKGINFO {
		t.Error(".PKGINFO not found in apk")
	}

	if !strings.Contains(pkginfoContent, "pkgname = hello") {
		t.Errorf("PKGINFO missing pkgname: %s", pkginfoContent)
	}
	if !strings.Contains(pkginfoContent, "pkgver = 1.0.0-r0") {
		t.Errorf("PKGINFO missing pkgver: %s", pkginfoContent)
	}
	if !strings.Contains(pkginfoContent, "depend = glibc") {
		t.Errorf("PKGINFO missing dependency: %s", pkginfoContent)
	}

	hasHello := false
	hasConf := false
	for _, f := range files {
		if strings.Contains(f, "hello") && strings.Contains(f, "bin") {
			hasHello = true
		}
		if strings.Contains(f, "hello.conf") {
			hasConf = true
		}
	}
	if !hasHello {
		t.Errorf("usr/bin/hello not found in apk, files: %v", files)
	}
	if !hasConf {
		t.Errorf("etc/hello.conf not found in apk, files: %v", files)
	}
}

func TestBuildDataTarOwners(t *testing.T) {
	destDir := t.TempDir()
	os.MkdirAll(filepath.Join(destDir, "home", "user"), 0755)
	os.MkdirAll(filepath.Join(destDir, "etc"), 0755)
	os.WriteFile(filepath.Join(destDir, "home", "user", ".profile"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(destDir, "etc", "conf"), []byte("y"), 0644)

	data, err := buildDataTar(destDir, map[string]string{
		"/home/user": "1000:1000",
		"/etc/conf":  "bogus",
	})
	if err != nil {
		t.Fatal(err)
	}

	owners := map[string][2]int{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		owners[hdr.Name] = [2]int{hdr.Uid, hdr.Gid}
	}

	for name, want := range map[string][2]int{
		"home":               {0, 0},
		"home/user":          {1000, 1000},
		"home/user/.profile": {1000, 1000},
		"etc/conf":           {0, 0},
	} {
		if got, ok := owners[name]; !ok || got != want {
			t.Errorf("%s: got %v (present=%v), want %v", name, got, ok, want)
		}
	}
}

func TestCreateAPK_EmptyDestDir(t *testing.T) {
	destDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "output")

	unit := &osbstar.Unit{
		Name:    "empty",
		Version: "1.0.0",
	}

	apkPath, err := CreateAPK(unit, destDir, "", outputDir, "x86_64", "", nil)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}

	if _, err := os.Stat(apkPath); os.IsNotExist(err) {
		t.Fatal("apk file not created for empty package")
	}
}

func TestCreateAPK_ServiceOnlyCompanion(t *testing.T) {
	destDir := t.TempDir()
	sysroot := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "output")

	initDir := filepath.Join(sysroot, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initDir, "docker"), []byte("#!/sbin/openrc-run\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	unit := &osbstar.Unit{
		Name:     "docker-enable",
		Version:  "1.0.0",
		Services: []string{"docker"},
	}

	apkPath, err := CreateAPK(unit, destDir, sysroot, outputDir, "x86_64", "", nil)
	if err != nil {
		t.Fatalf("CreateAPK: %v", err)
	}
	if _, err := os.Stat(apkPath); err != nil {
		t.Fatalf("apk not created: %v", err)
	}

	link := filepath.Join(destDir, "etc", "runlevels", "default", "docker")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected runlevel symlink at %s: %v", link, err)
	}
	if target != "/etc/init.d/docker" {
		t.Errorf("symlink target = %q, want /etc/init.d/docker", target)
	}
}

func TestCreateAPK_ServiceMissingInitScript(t *testing.T) {
	destDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "output")

	unit := &osbstar.Unit{
		Name:     "ghost-enable",
		Version:  "1.0.0",
		Services: []string{"ghost"},
	}

	_, err := CreateAPK(unit, destDir, "", outputDir, "x86_64", "", nil)
	if err == nil {
		t.Fatal("want error for service with no init script")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v, want service name in message", err)
	}
}

func TestGeneratePKGINFO(t *testing.T) {
	unit := &osbstar.Unit{
		Name:        "test",
		Version:     "2.0",
		Description: "Test package",
		License:     "BSD",
		RuntimeDeps: []string{"zlib", "openssl"},
	}

	info := generatePKGINFO(unit, t.TempDir(), "abc123", "x86_64", "deadbeef")

	if !strings.Contains(info, "pkgname = test") {
		t.Error("missing pkgname")
	}
	if !strings.Contains(info, "pkgver = 2.0-r0") {
		t.Error("missing pkgver")
	}
	if !strings.Contains(info, "pkgdesc = Test package") {
		t.Error("missing pkgdesc")
	}
	if !strings.Contains(info, "depend = zlib") {
		t.Error("missing depend zlib")
	}
	if !strings.Contains(info, "depend = openssl") {
		t.Error("missing depend openssl")
	}
	if !strings.Contains(info, "origin = test") {
		t.Errorf("missing origin: %s", info)
	}
	if !strings.Contains(info, "commit = deadbeef") {
		t.Errorf("missing commit: %s", info)
	}
}
