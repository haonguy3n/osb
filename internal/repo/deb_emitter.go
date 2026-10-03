package repo

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anhhao17/osb/internal/deb"
	"github.com/anhhao17/osb/internal/dpkg"
)

var debPublishMu sync.Mutex

type DebRepoOptions struct {
	RepoDir string

	Suite string

	Components []string

	Arches []string

	ValidUntilDays int

	GPGHomedir string
	GPGKeyID   string
}

func GenerateDebianIndex(opts DebRepoOptions) error {
	if opts.RepoDir == "" {
		return fmt.Errorf("deb_emitter: RepoDir is required")
	}
	if opts.Suite == "" {
		return fmt.Errorf("deb_emitter: Suite is required")
	}
	if len(opts.Components) == 0 {
		opts.Components = []string{"main"}
	}
	if len(opts.Arches) == 0 {
		return fmt.Errorf("deb_emitter: Arches is required")
	}
	if opts.ValidUntilDays == 0 {
		opts.ValidUntilDays = 30
	}

	distsDir := filepath.Join(opts.RepoDir, "dists", opts.Suite)
	var indices []packagesIndex

	for _, comp := range opts.Components {
		pooled, err := scanPool(filepath.Join(opts.RepoDir, "pool", comp), opts.RepoDir)
		if err != nil {
			return fmt.Errorf("deb_emitter: pool scan: %w", err)
		}
		byArch := map[string][]pooledDeb{}
		for _, pd := range pooled {
			byArch[pd.arch] = append(byArch[pd.arch], pd)
		}
		for _, arch := range opts.Arches {
			entries := append([]pooledDeb(nil), byArch[arch]...)
			entries = append(entries, byArch["all"]...)
			sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
			var bodyBuf bytes.Buffer
			for _, e := range entries {
				bodyBuf.Write(e.stanza)
				bodyBuf.WriteByte('\n')
			}
			body := bodyBuf.Bytes()
			rel := filepath.Join(comp, "binary-"+arch, "Packages")
			dst := filepath.Join(distsDir, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, body, 0o644); err != nil {
				return fmt.Errorf("write Packages: %w", err)
			}
			gz, err := gzipBytes(body)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dst+".gz", gz, 0o644); err != nil {
				return fmt.Errorf("write Packages.gz: %w", err)
			}
			indices = append(indices, packagesIndex{relPath: rel, body: body})
			indices = append(indices, packagesIndex{relPath: rel + ".gz", body: gz})
		}
	}

	releaseBody := buildRelease(opts, indices)
	releasePath := filepath.Join(distsDir, "Release")
	if err := os.MkdirAll(distsDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(releasePath, releaseBody, 0o644); err != nil {
		return fmt.Errorf("write Release: %w", err)
	}

	if opts.GPGHomedir != "" {
		signed, err := deb.SignInRelease(releaseBody, opts.GPGHomedir, opts.GPGKeyID)
		if err != nil {
			return fmt.Errorf("sign InRelease: %w", err)
		}
		if err := os.WriteFile(filepath.Join(distsDir, "InRelease"), signed, 0o644); err != nil {
			return fmt.Errorf("write InRelease: %w", err)
		}
	}
	return nil
}

type pooledDeb struct {
	arch   string
	path   string
	stanza []byte
}

func scanPool(componentPool, repoDir string) ([]pooledDeb, error) {
	var out []pooledDeb
	if _, err := os.Stat(componentPool); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.WalkDir(componentPool, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".deb") {
			return nil
		}
		stanza, arch, err := stanzaForDeb(repoDir, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		out = append(out, pooledDeb{arch: arch, path: p, stanza: stanza})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func stanzaForDeb(repoDir, debPath string) (stanza []byte, arch string, err error) {
	d, err := deb.ReadDeb(debPath)
	if err != nil {
		return nil, "", err
	}
	defer d.Close()
	arch = d.Control.Architecture

	relFilename, err := filepath.Rel(repoDir, debPath)
	if err != nil {
		return nil, "", err
	}
	stat, err := os.Stat(debPath)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(debPath)
	if err != nil {
		return nil, "", err
	}
	sha := sha256.Sum256(raw)

	var b bytes.Buffer
	if err := deb.WriteControl(&b, d.Control); err != nil {
		return nil, "", err
	}
	fmt.Fprintf(&b, "Filename: %s\n", filepath.ToSlash(relFilename))
	fmt.Fprintf(&b, "Size: %d\n", stat.Size())
	fmt.Fprintf(&b, "SHA256: %x\n", sha[:])
	return b.Bytes(), arch, nil
}

func buildRelease(opts DebRepoOptions, indices []packagesIndex) []byte {
	now := time.Now().UTC()
	validUntil := now.Add(time.Duration(opts.ValidUntilDays) * 24 * time.Hour)

	var b bytes.Buffer
	fmt.Fprintf(&b, "Origin: %s\n", opts.Suite)
	fmt.Fprintf(&b, "Label: %s\n", opts.Suite)
	fmt.Fprintf(&b, "Suite: %s\n", opts.Suite)
	fmt.Fprintf(&b, "Codename: %s\n", opts.Suite)
	fmt.Fprintf(&b, "Date: %s\n", now.Format(time.RFC1123))
	fmt.Fprintf(&b, "Valid-Until: %s\n", validUntil.Format(time.RFC1123))
	fmt.Fprintf(&b, "Components: %s\n", strings.Join(opts.Components, " "))
	fmt.Fprintf(&b, "Architectures: %s\n", strings.Join(opts.Arches, " "))
	fmt.Fprintln(&b, "Acquire-By-Hash: no")

	fmt.Fprintln(&b, "SHA256:")
	for _, idx := range indices {
		sum := sha256.Sum256(idx.body)
		fmt.Fprintf(&b, " %x %d %s\n", sum[:], len(idx.body), idx.relPath)
	}
	fmt.Fprintln(&b, "SHA512:")
	for _, idx := range indices {
		sum := sha512.Sum512(idx.body)
		fmt.Fprintf(&b, " %x %d %s\n", sum[:], len(idx.body), idx.relPath)
	}
	return b.Bytes()
}

type packagesIndex = struct {
	relPath string
	body    []byte
}

func gzipBytes(data []byte) ([]byte, error) {
	var b bytes.Buffer
	gw := gzip.NewWriter(&b)
	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func PublishDeb(debPath string, opts DebRepoOptions, component string) error {
	debPublishMu.Lock()
	defer debPublishMu.Unlock()

	d, err := deb.ReadDeb(debPath)
	if err != nil {
		return fmt.Errorf("PublishDeb: read %s: %w", debPath, err)
	}
	src := sourceNameOf(d.Control)
	_ = d.Close()
	initial := initialOf(src)
	poolDir := filepath.Join(opts.RepoDir, "pool", component, initial, src)
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(poolDir, filepath.Base(debPath))
	if err := copyFile(debPath, dst); err != nil {
		return fmt.Errorf("PublishDeb: copy: %w", err)
	}
	return nil
}

func sourceNameOf(c deb.Control) string {
	if c.Source == "" {
		return c.Package
	}
	return c.Source
}

func initialOf(src string) string {
	if strings.HasPrefix(src, "lib") && len(src) > 3 {
		return "lib" + string(src[3])
	}
	if src == "" {
		return "_"
	}
	return string(src[0])
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func VerifyMirrorSHA256(debPath, upstreamSHA256 string) error {
	if upstreamSHA256 == "" {
		return fmt.Errorf("VerifyMirrorSHA256: empty upstream SHA256")
	}
	raw, err := os.ReadFile(debPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	have := fmt.Sprintf("%x", sum[:])
	if !strings.EqualFold(have, upstreamSHA256) {
		return fmt.Errorf("VerifyMirrorSHA256: %s: computed %s != upstream %s", debPath, have, upstreamSHA256)
	}
	return nil
}

var _ = dpkg.ParseIndex
