package source

import (
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

var httpClient = &http.Client{
	Transport: &http.Transport{DisableCompression: true},
}

func decodeAPKChecksum(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "Q1") {
		return nil, fmt.Errorf("apk_checksum: expected Q1 (sha1) prefix, got %q", s)
	}
	raw, err := base64.StdEncoding.DecodeString(s[2:])
	if err != nil {
		return nil, fmt.Errorf("apk_checksum: base64 decode: %w", err)
	}
	if len(raw) != sha1.Size {
		return nil, fmt.Errorf("apk_checksum: wanted %d sha1 bytes, got %d",
			sha1.Size, len(raw))
	}
	return raw, nil
}

func apkControlSegment(data []byte) ([]byte, error) {
	bounds, err := gzipStreamBoundaries(data)
	if err != nil {
		return nil, fmt.Errorf("apk parse: %w", err)
	}
	if len(bounds) < 2 {
		return nil, fmt.Errorf("apk has %d gzip stream(s), expected >=2",
			len(bounds))
	}
	s2 := bounds[1]
	return data[s2[0]:s2[1]], nil
}

type gzipBound [2]int

func gzipStreamBoundaries(data []byte) ([]gzipBound, error) {
	var out []gzipBound
	pos := 0
	for pos < len(data) {
		if pos+10 > len(data) || data[pos] != 0x1f || data[pos+1] != 0x8b {
			break
		}
		start := pos
		flg := data[pos+3]
		hdrEnd := pos + 10
		if flg&0x04 != 0 {
			if hdrEnd+2 > len(data) {
				return nil, fmt.Errorf("truncated FEXTRA")
			}
			xlen := int(binary.LittleEndian.Uint16(data[hdrEnd : hdrEnd+2]))
			hdrEnd += 2 + xlen
		}
		if flg&0x08 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x10 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x02 != 0 {
			hdrEnd += 2
		}
		if hdrEnd > len(data) {
			return nil, fmt.Errorf("truncated gzip header")
		}
		br := bytes.NewReader(data[hdrEnd:])
		zr := flate.NewReader(br)
		if _, err := io.Copy(io.Discard, zr); err != nil {
			zr.Close()
			return nil, fmt.Errorf("deflate stream %d: %w", len(out), err)
		}
		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("deflate close stream %d: %w", len(out), err)
		}
		deflateConsumed := (len(data) - hdrEnd) - br.Len()
		end := hdrEnd + deflateConsumed + 8
		if end > len(data) {
			return nil, fmt.Errorf("truncated gzip trailer")
		}
		out = append(out, gzipBound{start, end})
		pos = end
	}
	return out, nil
}

func CacheDir() (string, error) {
	dir := os.Getenv("OSB_CACHE")
	if dir == "" {
		dir = "cache"
	}
	dir = filepath.Join(dir, "sources")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func Fetch(unit *osbstar.Unit, w io.Writer) (string, error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return "", err
	}

	if unit.Source == "" {
		return "", fmt.Errorf("unit %q has no source", unit.Name)
	}

	if isGitURL(unit.Source) {
		return fetchGit(cacheDir, unit, w)
	}
	return fetchHTTP(cacheDir, unit, w)
}

func fetchHTTP(cacheDir string, unit *osbstar.Unit, w io.Writer) (string, error) {
	urlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(unit.Source)))
	ext := guessExt(unit.Source)
	cachedPath := filepath.Join(cacheDir, urlHash+ext)

	if _, err := os.Stat(cachedPath); err == nil {
		return cachedPath, nil
	}

	fmt.Fprintf(w, "Fetching %s...\n", unit.Source)

	resp, err := httpClient.Get(unit.Source)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", unit.Source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", unit.Source, resp.StatusCode)
	}

	var apkExpected []byte
	if unit.APKChecksum != "" {
		raw, err := decodeAPKChecksum(unit.APKChecksum)
		if err != nil {
			return "", fmt.Errorf("unit %q: %w", unit.Name, err)
		}
		apkExpected = raw
	}

	tmp, err := os.CreateTemp(cacheDir, "download-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	h256 := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h256), resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("downloading %s: %w", unit.Source, err)
	}
	tmp.Close()

	switch {
	case unit.SHA256 != "":
		actual := fmt.Sprintf("%x", h256.Sum(nil))
		if actual != unit.SHA256 {
			os.Remove(tmpPath)
			return "", fmt.Errorf("SHA256 mismatch:\n  expected %s\n  got      %s",
				unit.SHA256, actual)
		}
	case unit.APKChecksum != "":
		raw, err := os.ReadFile(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("reading %s for apk_checksum verify: %w",
				tmpPath, err)
		}
		ctrl, err := apkControlSegment(raw)
		if err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("apk_checksum verify: %w", err)
		}
		actualRaw := sha1.Sum(ctrl)
		if !bytes.Equal(actualRaw[:], apkExpected) {
			os.Remove(tmpPath)
			return "", fmt.Errorf("apk_checksum mismatch:\n  expected Q1%s\n  got      Q1%s",
				base64.StdEncoding.EncodeToString(apkExpected),
				base64.StdEncoding.EncodeToString(actualRaw[:]))
		}
	}

	if err := os.Rename(tmpPath, cachedPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	return cachedPath, nil
}

func fetchGit(cacheDir string, unit *osbstar.Unit, w io.Writer) (string, error) {
	ref := unit.Tag
	if ref == "" {
		ref = unit.Branch
	}
	if ref == "" {
		ref = "HEAD"
	}
	cacheKey := unit.Source + "#" + ref
	urlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(cacheKey)))
	barePath := filepath.Join(cacheDir, urlHash+".git")

	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		fmt.Fprintf(w, "Cloning %s (ref: %s)...\n", unit.Source, ref)

		args := []string{"clone", "--bare", "--depth", "1"}
		if unit.Tag != "" {
			args = append(args, "--branch", unit.Tag)
		} else if unit.Branch != "" {
			args = append(args, "--branch", unit.Branch)
		}
		args = append(args, unit.Source, barePath)

		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone %s: %s\n%s", unit.Source, err, out)
		}
	} else {
		fmt.Fprintf(w, "Using cached %s (ref: %s)\n", unit.Source, ref)
	}

	return barePath, nil
}

func isGitURL(url string) bool {
	return strings.HasSuffix(url, ".git") ||
		strings.HasPrefix(url, "git://") ||
		strings.HasPrefix(url, "git@") ||
		(strings.Contains(url, "github.com/") && !strings.Contains(url, "/archive/") && !strings.Contains(url, "/releases/"))
}

func guessExt(url string) string {
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".zip", ".apk", ".deb"} {
		if strings.HasSuffix(url, ext) {
			return ext
		}
	}
	return ".tar.gz"
}
