package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"debug/elf"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func CreateAPK(unit *osbstar.Unit, destDir, sysroot, outputDir, arch, commit string, signer *Signer) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}

	apkName := fmt.Sprintf("%s-%s-r%d.apk", unit.Name, unit.Version, unit.Release)
	apkPath := filepath.Join(outputDir, apkName)

	if err := materializeServiceSymlinks(unit, destDir, sysroot); err != nil {
		return "", fmt.Errorf("creating service symlinks: %w", err)
	}

	dataTar, err := buildDataTar(destDir, unit.Owners)
	if err != nil {
		return "", fmt.Errorf("building data tar: %w", err)
	}
	var dataGz bytes.Buffer
	gw := gzip.NewWriter(&dataGz)
	if _, err := gw.Write(dataTar); err != nil {
		return "", fmt.Errorf("compressing data tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("closing data tar gzip: %w", err)
	}
	dataHash := sha256.Sum256(dataGz.Bytes())
	dataHashHex := fmt.Sprintf("%x", dataHash[:])

	pkginfo := generatePKGINFO(unit, destDir, dataHashHex, arch, commit)

	var controlGz bytes.Buffer
	if err := writeGzipTar(&controlGz, map[string][]byte{".PKGINFO": []byte(pkginfo)}); err != nil {
		return "", fmt.Errorf("building control stream: %w", err)
	}

	f, err := os.Create(apkPath)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", apkPath, err)
	}
	defer f.Close()

	if signer != nil {
		sigGz, err := signer.SignStream(controlGz.Bytes())
		if err != nil {
			return "", fmt.Errorf("signing control stream: %w", err)
		}
		if _, err := f.Write(sigGz); err != nil {
			return "", fmt.Errorf("writing signature stream: %w", err)
		}
	}

	if _, err := f.Write(controlGz.Bytes()); err != nil {
		return "", fmt.Errorf("writing control stream: %w", err)
	}
	if _, err := f.Write(dataGz.Bytes()); err != nil {
		return "", fmt.Errorf("writing data stream: %w", err)
	}

	return apkPath, nil
}

func RepackAPK(unit *osbstar.Unit, srcAPK, outputDir string, signer *Signer) (string, error) {
	if signer == nil {
		return "", fmt.Errorf("RepackAPK requires a signer")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}
	apkName := fmt.Sprintf("%s-%s-r%d.apk", unit.Name, unit.Version, unit.Release)
	apkPath := filepath.Join(outputDir, apkName)

	raw, err := os.ReadFile(srcAPK)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", srcAPK, err)
	}

	streams, err := splitGzipStreams(raw)
	if err != nil {
		return "", fmt.Errorf("splitting %s into gzip streams: %w", srcAPK, err)
	}
	if len(streams) < 2 {
		return "", fmt.Errorf("%s: expected at least 2 gzip streams (control+data), got %d", srcAPK, len(streams))
	}

	idx := 0
	if isSignatureStream(streams[0]) {
		idx = 1
	}
	if len(streams)-idx < 2 {
		return "", fmt.Errorf("%s: missing control or data stream after signature strip", srcAPK)
	}
	control := streams[idx]
	data := bytes.Join(streams[idx+1:], nil)

	sigGz, err := signer.SignStream(control)
	if err != nil {
		return "", fmt.Errorf("signing control stream: %w", err)
	}

	f, err := os.Create(apkPath)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", apkPath, err)
	}
	defer f.Close()
	if _, err := f.Write(sigGz); err != nil {
		return "", fmt.Errorf("writing signature stream: %w", err)
	}
	if _, err := f.Write(control); err != nil {
		return "", fmt.Errorf("writing control stream: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("writing data stream: %w", err)
	}
	return apkPath, nil
}

func splitGzipStreams(raw []byte) ([][]byte, error) {
	var out [][]byte
	r := bytes.NewReader(raw)
	for r.Len() > 0 {
		start := int64(len(raw)) - int64(r.Len())
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("gzip stream at offset %d: %w", start, err)
		}
		gr.Multistream(false)
		if _, err := io.Copy(io.Discard, gr); err != nil {
			gr.Close()
			return nil, fmt.Errorf("reading gzip stream at offset %d: %w", start, err)
		}
		if err := gr.Close(); err != nil {
			return nil, fmt.Errorf("closing gzip stream at offset %d: %w", start, err)
		}
		end := int64(len(raw)) - int64(r.Len())
		out = append(out, raw[start:end])
	}
	return out, nil
}

func ReadAPKArch(srcAPK string) (string, error) {
	raw, err := os.ReadFile(srcAPK)
	if err != nil {
		return "", err
	}
	streams, err := splitGzipStreams(raw)
	if err != nil {
		return "", err
	}
	idx := 0
	if len(streams) > 0 && isSignatureStream(streams[0]) {
		idx = 1
	}
	if idx >= len(streams) {
		return "", fmt.Errorf("%s: no control stream", srcAPK)
	}
	gr, err := gzip.NewReader(bytes.NewReader(streams[idx]))
	if err != nil {
		return "", err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != ".PKGINFO" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "arch = ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "arch = ")), nil
			}
		}
		break
	}
	return "", fmt.Errorf("%s: arch not found in PKGINFO", srcAPK)
}

func scanSONAMEs(destDir string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	err := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		ef, err := elf.NewFile(f)
		if err != nil {
			return nil
		}
		defer ef.Close()
		sonames, err := ef.DynString(elf.DT_SONAME)
		if err != nil {
			return nil
		}
		for _, s := range sonames {
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func isSignatureStream(streamGz []byte) bool {
	gr, err := gzip.NewReader(bytes.NewReader(streamGz))
	if err != nil {
		return false
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		return false
	}
	return strings.HasPrefix(hdr.Name, ".SIGN.RSA.")
}

func normalizeOwnership(h *tar.Header) {
	h.Uid = 0
	h.Gid = 0
	h.Uname = "root"
	h.Gname = "root"
}

func applyOwners(h *tar.Header, rel string, owners map[string]string) {
	if len(owners) == 0 {
		return
	}
	abs := "/" + filepath.ToSlash(rel)
	for path, ug := range owners {
		if abs != path && !strings.HasPrefix(abs, path+"/") {
			continue
		}
		uidStr, gidStr, ok := strings.Cut(ug, ":")
		uid, uerr := strconv.Atoi(uidStr)
		gid, gerr := strconv.Atoi(gidStr)
		if !ok || uerr != nil || gerr != nil {
			continue
		}
		h.Uid = uid
		h.Gid = gid
		h.Uname = ""
		h.Gname = ""
		return
	}
}

func buildDataTar(destDir string, owners map[string]string) ([]byte, error) {
	var paths []string
	if err := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == destDir {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(paths)

	tmp, err := os.CreateTemp("", "osb-data-*.tar")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	tw := tar.NewWriter(tmp)
	for _, path := range paths {
		rel, _ := filepath.Rel(destDir, path)
		info, err := os.Lstat(path)
		if err != nil {
			tmp.Close()
			return nil, err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			tmp.Close()
			return nil, err
		}
		header.Name = rel
		header.ModTime = SourceDateEpoch()
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		normalizeOwnership(header)
		applyOwners(header, rel, owners)

		if info.Mode()&os.ModeSymlink != 0 {
			link, _ := os.Readlink(path)
			header.Linkname = link
			header.Typeflag = tar.TypeSymlink
		}

		var content []byte
		if info.Mode().IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				tmp.Close()
				return nil, err
			}
			sum := sha1.Sum(content)
			if header.PAXRecords == nil {
				header.PAXRecords = map[string]string{}
			}
			header.PAXRecords["APK-TOOLS.checksum.SHA1"] = fmt.Sprintf("%x", sum[:])
		} else if info.Mode()&os.ModeSymlink != 0 {
			sum := sha1.Sum([]byte(header.Linkname))
			if header.PAXRecords == nil {
				header.PAXRecords = map[string]string{}
			}
			header.PAXRecords["APK-TOOLS.checksum.SHA1"] = fmt.Sprintf("%x", sum[:])
		}

		if err := tw.WriteHeader(header); err != nil {
			tmp.Close()
			return nil, err
		}

		if content != nil {
			if _, err := tw.Write(content); err != nil {
				tmp.Close()
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	return os.ReadFile(tmpName)
}

func writeGzipTar(w io.Writer, files map[string][]byte) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		content := files[name]
		header := &tar.Header{
			Name:    name,
			Size:    int64(len(content)),
			Mode:    0644,
			ModTime: SourceDateEpoch(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(content); err != nil {
			return err
		}
	}

	if err := tw.Flush(); err != nil {
		return err
	}
	return gw.Close()
}

func generatePKGINFO(unit *osbstar.Unit, destDir, dataHashHex, arch, commit string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "pkgname = %s\n", unit.Name)
	fmt.Fprintf(&b, "pkgver = %s-r%d\n", unit.Version, unit.Release)

	if unit.Description != "" {
		fmt.Fprintf(&b, "pkgdesc = %s\n", unit.Description)
	}
	if unit.License != "" {
		fmt.Fprintf(&b, "license = %s\n", unit.License)
	}

	fmt.Fprintf(&b, "arch = %s\n", arch)
	fmt.Fprintf(&b, "builddate = %d\n", SourceDateEpoch().Unix())

	fmt.Fprintf(&b, "origin = %s\n", unit.Name)

	if commit != "" {
		fmt.Fprintf(&b, "commit = %s\n", commit)
	}

	var size int64
	filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	fmt.Fprintf(&b, "size = %d\n", size)

	if dataHashHex != "" {
		fmt.Fprintf(&b, "datahash = %s\n", dataHashHex)
	}

	for _, dep := range unit.RuntimeDepsForDistro("alpine") {
		fmt.Fprintf(&b, "depend = %s\n", dep)
	}

	soVersion := fmt.Sprintf("%s-r%d", unit.Version, unit.Release)
	for _, p := range unit.Provides {
		if strings.ContainsAny(p, "=<>~") {
			fmt.Fprintf(&b, "provides = %s\n", p)
		} else {
			fmt.Fprintf(&b, "provides = %s=%s\n", p, soVersion)
		}
	}

	if sonames, err := scanSONAMEs(destDir); err == nil {
		for _, s := range sonames {
			fmt.Fprintf(&b, "provides = so:%s=%s\n", s, soVersion)
		}
	}

	for _, r := range unit.Replaces {
		fmt.Fprintf(&b, "replaces = %s\n", r)
	}

	return b.String()
}

func materializeServiceSymlinks(unit *osbstar.Unit, destDir, sysroot string) error {
	if len(unit.Services) == 0 {
		return nil
	}
	runlevel := filepath.Join(destDir, "etc", "runlevels", "default")
	for _, svc := range unit.Services {
		if !initScriptAvailable(destDir, sysroot, svc) {
			return fmt.Errorf("service %q declared but /etc/init.d/%s missing in destdir or sysroot", svc, svc)
		}
		linkPath := filepath.Join(runlevel, svc)
		if _, err := os.Lstat(linkPath); err == nil {
			continue
		}
		if err := os.MkdirAll(runlevel, 0755); err != nil {
			return err
		}
		if err := os.Symlink("/etc/init.d/"+svc, linkPath); err != nil {
			return err
		}
	}
	return nil
}

func initScriptAvailable(destDir, sysroot, svc string) bool {
	if _, err := os.Stat(filepath.Join(destDir, "etc", "init.d", svc)); err == nil {
		return true
	}
	if sysroot != "" {
		if _, err := os.Stat(filepath.Join(sysroot, "etc", "init.d", svc)); err == nil {
			return true
		}
	}
	return false
}
