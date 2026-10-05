package repo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anhhao17/osb/internal/artifact"
)

func GenerateIndex(repoDir string, signer *artifact.Signer) error {
	type apkEntry struct {
		dir  string
		name string
	}
	var apks []apkEntry
	collect := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".apk") {
				apks = append(apks, apkEntry{dir: dir, name: e.Name()})
			}
		}
		return nil
	}
	if err := collect(repoDir); err != nil {
		return fmt.Errorf("reading repo dir: %w", err)
	}
	if filepath.Base(repoDir) != "noarch" {
		noarchDir := filepath.Join(filepath.Dir(repoDir), "noarch")
		if err := collect(noarchDir); err != nil {
			return fmt.Errorf("reading noarch dir: %w", err)
		}
	}
	sort.Slice(apks, func(i, j int) bool { return apks[i].name < apks[j].name })

	if len(apks) == 0 {
		return nil
	}

	var buf strings.Builder
	for i, e := range apks {
		name := e.name
		apkPath := filepath.Join(e.dir, name)

		info, err := os.Stat(apkPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", name, err)
		}

		hash, err := sha1base64(apkPath)
		if err != nil {
			return fmt.Errorf("hashing %s: %w", name, err)
		}

		pkginfo := extractPKGINFO(apkPath)
		pkgName := pkginfo.Get("pkgname")
		version := pkginfo.Get("pkgver")
		scope := pkginfo.Get("arch")
		installedSize := pkginfoSize(pkginfo)
		description := pkginfo.Get("pkgdesc")
		license := pkginfo.Get("license")
		buildDate := pkginfo.Get("builddate")
		origin := pkginfo.Get("origin")
		commit := pkginfo.Get("commit")
		url := pkginfo.Get("url")
		depends := strings.Join(pkginfo.values("depend"), " ")
		provides := strings.Join(pkginfo.values("provides"), " ")
		replaces := strings.Join(pkginfo.values("replaces"), " ")

		fmt.Fprintf(&buf, "C:Q1%s\n", hash)
		fmt.Fprintf(&buf, "P:%s\n", pkgName)
		fmt.Fprintf(&buf, "V:%s\n", version)
		fmt.Fprintf(&buf, "A:%s\n", scope)
		fmt.Fprintf(&buf, "S:%d\n", info.Size())
		fmt.Fprintf(&buf, "I:%d\n", installedSize)
		fmt.Fprintf(&buf, "T:%s\n", description)
		if url != "" {
			fmt.Fprintf(&buf, "U:%s\n", url)
		}
		if license != "" {
			fmt.Fprintf(&buf, "L:%s\n", license)
		}
		if origin != "" {
			fmt.Fprintf(&buf, "o:%s\n", origin)
		}
		if buildDate != "" {
			fmt.Fprintf(&buf, "t:%s\n", buildDate)
		}
		if commit != "" {
			fmt.Fprintf(&buf, "c:%s\n", commit)
		}
		if depends != "" {
			fmt.Fprintf(&buf, "D:%s\n", depends)
		}
		if provides != "" {
			fmt.Fprintf(&buf, "p:%s\n", provides)
		}
		if replaces != "" {
			fmt.Fprintf(&buf, "r:%s\n", replaces)
		}
		if i < len(apks)-1 {
			buf.WriteString("\n")
		}
	}

	var indexBuf bytes.Buffer
	gw := gzip.NewWriter(&indexBuf)
	tw := tar.NewWriter(gw)

	content := []byte(buf.String())
	hdr := &tar.Header{
		Name:    "APKINDEX",
		Size:    int64(len(content)),
		Mode:    0644,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("writing tar header: %w", err)
	}
	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("writing tar content: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing index tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("closing index gzip: %w", err)
	}

	indexPath := filepath.Join(repoDir, "APKINDEX.tar.gz")
	f, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("creating APKINDEX.tar.gz: %w", err)
	}
	defer f.Close()

	if signer != nil {
		sigGz, err := signer.SignStream(indexBuf.Bytes())
		if err != nil {
			return fmt.Errorf("signing APKINDEX: %w", err)
		}
		if _, err := f.Write(sigGz); err != nil {
			return fmt.Errorf("writing index signature: %w", err)
		}
	}
	if _, err := f.Write(indexBuf.Bytes()); err != nil {
		return fmt.Errorf("writing APKINDEX body: %w", err)
	}

	return nil
}

func sha1base64(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	streamStart := int64(0)
	consumed, isSig, err := streamLenAndIsSig(f, streamStart)
	if err != nil {
		return "", err
	}
	if isSig {
		streamStart += consumed
		if _, err := f.Seek(streamStart, 0); err != nil {
			return "", err
		}
		consumed, _, err = streamLenAndIsSig(f, streamStart)
		if err != nil {
			return "", err
		}
	}

	f2, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f2.Close()
	if _, err := f2.Seek(streamStart, 0); err != nil {
		return "", err
	}

	h := sha1.New()
	if _, err := io.CopyN(h, f2, consumed); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

func streamLenAndIsSig(f *os.File, _ int64) (int64, bool, error) {
	cr := &countingReader{r: &oneByteReader{r: f}}
	gr, err := gzip.NewReader(cr)
	if err != nil {
		return 0, false, err
	}
	gr.Multistream(false)

	tr := tar.NewReader(gr)
	hdr, terr := tr.Next()
	isSig := terr == nil && hdr != nil && strings.HasPrefix(hdr.Name, ".SIGN.")

	if _, err := io.Copy(io.Discard, gr); err != nil {
		return 0, false, err
	}
	if err := gr.Close(); err != nil {
		return 0, false, err
	}
	return cr.n, isSig, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

type pkginfoMap map[string][]string

func (p pkginfoMap) values(key string) []string { return p[key] }

func (p pkginfoMap) Get(key string) string {
	v := p[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func extractPKGINFO(apkPath string) pkginfoMap {
	out := pkginfoMap{}
	f, err := os.Open(apkPath)
	if err != nil {
		return out
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return out
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return out
		}
		if hdr.Name == ".PKGINFO" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return out
			}
			for _, line := range strings.Split(string(data), "\n") {
				idx := strings.Index(line, " = ")
				if idx < 0 {
					continue
				}
				key := line[:idx]
				val := line[idx+3:]
				out[key] = append(out[key], val)
			}
			return out
		}
	}
}

func pkginfoSize(p pkginfoMap) int64 {
	val := p.Get("size")
	if val == "" {
		return 0
	}
	var n int64
	fmt.Sscanf(val, "%d", &n)
	return n
}
