package alpine

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/anhhao17/osb/internal/apkindex"
)

type UpdateOptions struct {
	ModuleDir string

	Arches []string

	HTTPClient *http.Client

	Out io.Writer
}

func UpdateFeeds(opts UpdateOptions) error {
	if opts.ModuleDir == "" {
		return fmt.Errorf("update-feeds: ModuleDir is required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}

	decls, err := PeekFeedDecls(opts.ModuleDir)
	if err != nil {
		return err
	}
	if len(decls) == 0 {
		return fmt.Errorf("update-feeds: no alpine_feed() calls in %s/MODULE.star", opts.ModuleDir)
	}

	totalWritten := 0
	totalBytes := int64(0)
	for _, d := range decls {
		fmt.Fprintf(opts.Out, "→ %s (%s, %s)\n", d.Name, d.Branch, d.URL)
		arches := pickArches(opts, d)
		if len(arches) == 0 {
			return fmt.Errorf("update-feeds: %s: no arches to fetch (set --arch or pre-create feed dirs)", d.Name)
		}
		trustedKeys, err := resolveKeyPaths(opts.ModuleDir, d.Keys)
		if err != nil {
			return fmt.Errorf("update-feeds: %s: %w", d.Name, err)
		}
		if len(trustedKeys) == 0 {
			return fmt.Errorf("update-feeds: %s: alpine_feed must declare keys=[...] for signature verification", d.Name)
		}
		for _, osbArch := range arches {
			alpineArch, ok := archMap[osbArch]
			if !ok {
				return fmt.Errorf("update-feeds: %s: unsupported arch %q", d.Name, osbArch)
			}
			n, err := fetchOne(opts, d, osbArch, alpineArch, trustedKeys)
			if err != nil {
				return fmt.Errorf("update-feeds: %s/%s: %w", d.Name, alpineArch, err)
			}
			totalWritten++
			totalBytes += n
		}
	}
	fmt.Fprintf(opts.Out, "\nWrote %d APKINDEX file(s), %s total.\n",
		totalWritten, humanBytes(totalBytes))
	fmt.Fprintf(opts.Out, "Review with `git diff` and commit when ready.\n")
	return nil
}

func pickArches(opts UpdateOptions, d FeedDecl) []string {
	if len(opts.Arches) > 0 {
		return opts.Arches
	}
	indexDir := filepath.Join(opts.ModuleDir, d.Index)
	entries, err := os.ReadDir(indexDir)
	if err == nil {
		var existing []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for osbArch, alpineArch := range archMap {
				if e.Name() == alpineArch {
					existing = append(existing, osbArch)
					break
				}
			}
		}
		if len(existing) > 0 {
			sort.Strings(existing)
			return existing
		}
	}
	all := supportedArches()
	sort.Strings(all)
	return all
}

func resolveKeyPaths(moduleDir string, relPaths []string) ([]string, error) {
	out := make([]string, 0, len(relPaths))
	for _, rel := range relPaths {
		p := rel
		if !filepath.IsAbs(p) {
			p = filepath.Join(moduleDir, rel)
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("key file %s: %w", rel, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func fetchOne(opts UpdateOptions, d FeedDecl, osbArch, alpineArch string, trustedKeys []string) (int64, error) {
	url := fmt.Sprintf("%s/%s/%s/%s/APKINDEX.tar.gz", d.URL, d.Branch, d.Section, alpineArch)
	fmt.Fprintf(opts.Out, "  %s: fetching %s\n", osbArch, url)

	resp, err := opts.HTTPClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tarball, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
	}

	if err := apkindex.VerifySignatureBytes(tarball, trustedKeys); err != nil {
		return 0, fmt.Errorf("signature: %w", err)
	}

	indexBytes, err := extractInnerAPKINDEX(tarball)
	if err != nil {
		return 0, fmt.Errorf("extract APKINDEX: %w", err)
	}

	dst := filepath.Join(opts.ModuleDir, d.Index, alpineArch, "APKINDEX")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	if err := atomicWrite(dst, indexBytes); err != nil {
		return 0, err
	}

	entryCount := countEntries(indexBytes)
	fmt.Fprintf(opts.Out, "  %s: wrote %s (%d entries, signed by %s)\n",
		osbArch, relTo(dst, opts.ModuleDir), entryCount, sigKeyName(trustedKeys[0]))
	return int64(len(tarball)), nil
}

func extractInnerAPKINDEX(tarball []byte) ([]byte, error) {
	bounds, err := gzipStreamBoundaries(tarball)
	if err != nil {
		return nil, err
	}
	for _, b := range bounds {
		stream := tarball[b[0]:b[1]]
		index, err := extractAPKINDEXFromStream(stream)
		if err != nil {
			return nil, err
		}
		if index != nil {
			return index, nil
		}
	}
	return nil, fmt.Errorf("no APKINDEX entry in tarball")
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create tmpfile: %w", err)
	}
	defer func() {
		if _, statErr := os.Stat(tmp); statErr == nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func countEntries(index []byte) int {
	n := 0
	atStart := true
	for i := 0; i < len(index); i++ {
		if atStart && index[i] == 'P' && i+1 < len(index) && index[i+1] == ':' {
			n++
		}
		atStart = index[i] == '\n'
	}
	return n
}

func relTo(path, base string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

func sigKeyName(keyPath string) string { return filepath.Base(keyPath) }

func humanBytes(n int64) string {
	const (
		KiB = 1024
		MiB = 1024 * 1024
		GiB = 1024 * 1024 * 1024
	)
	switch {
	case n < KiB:
		return fmt.Sprintf("%d B", n)
	case n < MiB:
		return fmt.Sprintf("%.1f KiB", float64(n)/KiB)
	case n < GiB:
		return fmt.Sprintf("%.1f MiB", float64(n)/MiB)
	default:
		return fmt.Sprintf("%.2f GiB", float64(n)/GiB)
	}
}

var _ = sha256.Size
