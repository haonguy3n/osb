package dpkg

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"pault.ag/go/debian/control"
)

type Entry struct {
	Package       string
	Source        string
	Version       string
	Architecture  string
	MultiArch     string
	Maintainer    string
	Description   string
	Section       string
	Priority      string
	Homepage      string
	InstalledSize int
	Size          int

	Filename string

	SHA256 string
	SHA1   string
	MD5sum string

	Depends    string
	PreDepends string `control:"Pre-Depends"`
	Recommends string
	Suggests   string
	Enhances   string
	Conflicts  string
	Breaks     string
	Replaces   string
	Provides   string
}

func ParseIndex(r io.Reader) ([]Entry, error) {
	type stanza struct {
		Package       string
		Source        string
		Version       string
		Architecture  string
		MultiArch     string `control:"Multi-Arch"`
		Maintainer    string
		Description   string
		Section       string
		Priority      string
		Homepage      string
		InstalledSize int `control:"Installed-Size"`
		Size          int
		Filename      string
		SHA256        string
		SHA1          string
		MD5sum        string
		Depends       string
		PreDepends    string `control:"Pre-Depends"`
		Recommends    string
		Suggests      string
		Enhances      string
		Conflicts     string
		Breaks        string
		Replaces      string
		Provides      string
	}
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 64*1024)
	}
	var raw []stanza
	if err := control.Unmarshal(&raw, br); err != nil {
		return nil, fmt.Errorf("dpkg: parse Packages: %w", err)
	}
	out := make([]Entry, 0, len(raw))
	for i, s := range raw {
		if s.Package == "" {
			return nil, fmt.Errorf("dpkg: stanza %d: missing Package field", i)
		}
		out = append(out, Entry{
			Package:       s.Package,
			Source:        s.Source,
			Version:       s.Version,
			Architecture:  s.Architecture,
			MultiArch:     s.MultiArch,
			Maintainer:    s.Maintainer,
			Description:   s.Description,
			Section:       s.Section,
			Priority:      s.Priority,
			Homepage:      s.Homepage,
			InstalledSize: s.InstalledSize,
			Size:          s.Size,
			Filename:      s.Filename,
			SHA256:        s.SHA256,
			SHA1:          s.SHA1,
			MD5sum:        s.MD5sum,
			Depends:       s.Depends,
			PreDepends:    s.PreDepends,
			Recommends:    s.Recommends,
			Suggests:      s.Suggests,
			Enhances:      s.Enhances,
			Conflicts:     s.Conflicts,
			Breaks:        s.Breaks,
			Replaces:      s.Replaces,
			Provides:      s.Provides,
		})
	}
	return out, nil
}

func ParseIndexFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseIndex(f)
}
