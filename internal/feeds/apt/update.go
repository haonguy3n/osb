package apt

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anhhao17/osb/internal/dpkg"
)

type UpdateOptions struct {
	ModuleDir string

	Arches []string

	HTTPClient *http.Client

	Out io.Writer

	AllowKeyUpdate string
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
		return fmt.Errorf("update-feeds: no apt_feed() calls in %s/MODULE.star", opts.ModuleDir)
	}

	if opts.AllowKeyUpdate != "" {
		if err := appendAllowedFingerprint(opts.ModuleDir, opts.AllowKeyUpdate); err != nil {
			return fmt.Errorf("update-feeds: --allow-key-update: %w", err)
		}
	}

	totalWritten := 0
	totalBytes := int64(0)
	for _, d := range decls {
		fmt.Fprintf(opts.Out, "→ %s (%s, %s)\n", d.Name, d.Suite, d.URL)
		arches := pickArches(opts, d)
		if len(arches) == 0 {
			return fmt.Errorf("update-feeds: %s: no arches to fetch (set --arch or pre-create feed dirs)", d.Name)
		}
		keyring, err := readKeyring(opts.ModuleDir, d.Keyring)
		if err != nil {
			return fmt.Errorf("update-feeds: %s: %w", d.Name, err)
		}
		allowed, err := readAllowedFingerprints(opts.ModuleDir)
		if err != nil {
			return fmt.Errorf("update-feeds: %s: %w", d.Name, err)
		}

		inReleaseURL := fmt.Sprintf("%s/dists/%s/InRelease",
			strings.TrimSuffix(d.URL, "/"), d.Suite)
		fmt.Fprintf(opts.Out, "  fetching %s\n", inReleaseURL)
		inRelease, err := httpGet(opts.HTTPClient, inReleaseURL)
		if err != nil {
			return fmt.Errorf("update-feeds: %s: InRelease: %w", d.Name, err)
		}
		if err := enforceAllowList(inRelease, allowed); err != nil {
			return fmt.Errorf("update-feeds: %s: %w", d.Name, err)
		}
		body, err := dpkg.VerifyInRelease(inRelease, keyring)
		if err != nil {
			return fmt.Errorf("update-feeds: %s: InRelease verify: %w", d.Name, err)
		}
		_ = body

		for _, osbArch := range arches {
			debArch, ok := archMap[osbArch]
			if !ok {
				return fmt.Errorf("update-feeds: %s: unsupported arch %q", d.Name, osbArch)
			}
			n, err := fetchPackages(opts, d, osbArch, debArch)
			if err != nil {
				return fmt.Errorf("update-feeds: %s/%s: %w", d.Name, debArch, err)
			}
			totalWritten++
			totalBytes += n
		}
	}
	fmt.Fprintf(opts.Out, "\nWrote %d Packages file(s), %s total.\n", totalWritten, humanBytes(totalBytes))
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
			for osbArch, debArch := range archMap {
				if e.Name() == debArch {
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
	var out []string
	for _, declArch := range d.Arches {
		for osbArch, debArch := range archMap {
			if debArch == declArch {
				out = append(out, osbArch)
				break
			}
		}
	}
	if len(out) > 0 {
		sort.Strings(out)
		return out
	}
	all := supportedArches()
	sort.Strings(all)
	return all
}

func fetchPackages(opts UpdateOptions, d FeedDecl, osbArch, debArch string) (int64, error) {
	url := fmt.Sprintf("%s/dists/%s/%s/binary-%s/Packages.gz",
		strings.TrimSuffix(d.baseURLFor(osbArch), "/"), d.Suite, d.Component, debArch)
	fmt.Fprintf(opts.Out, "  %s: fetching %s\n", osbArch, url)

	gz, err := httpGet(opts.HTTPClient, url)
	if err != nil {
		return 0, err
	}

	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	raw, err := io.ReadAll(gr)
	if err != nil {
		return 0, fmt.Errorf("decompress: %w", err)
	}

	dst := filepath.Join(opts.ModuleDir, d.Index, debArch, "Packages")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	if err := atomicWrite(dst, raw); err != nil {
		return 0, err
	}
	entryCount := countStanzas(raw)
	fmt.Fprintf(opts.Out, "  %s: wrote %s (%d entries)\n", osbArch, relTo(dst, opts.ModuleDir), entryCount)
	return int64(len(gz)), nil
}

func httpGet(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func readKeyring(moduleDir, rel string) ([]byte, error) {
	if rel == "" {
		return nil, fmt.Errorf("apt_feed must declare keyring=... for signature verification")
	}
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(moduleDir, rel)
	}
	return os.ReadFile(p)
}

func readAllowedFingerprints(moduleDir string) (map[string]bool, error) {
	path := filepath.Join(moduleDir, "keys", "allowed-fingerprints")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("allowed-fingerprints: %w", err)
	}
	defer f.Close()

	allowed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fpr := strings.ToUpper(strings.ReplaceAll(line, " ", ""))
		allowed[fpr] = true
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return allowed, nil
}

func enforceAllowList(_ []byte, _ map[string]bool) error {
	return nil
}

func appendAllowedFingerprint(moduleDir, fpr string) error {
	fpr = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fpr), " ", ""))
	if fpr == "" {
		return fmt.Errorf("empty fingerprint")
	}
	dir := filepath.Join(moduleDir, "keys")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "allowed-fingerprints")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# Added via --allow-key-update\n%s\n", fpr)
	return err
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
	return os.Rename(tmp, path)
}

func countStanzas(data []byte) int {
	n := 0
	atStart := true
	for i := 0; i < len(data); i++ {
		if atStart && i+8 <= len(data) && string(data[i:i+8]) == "Package:" {
			n++
		}
		atStart = data[i] == '\n'
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
