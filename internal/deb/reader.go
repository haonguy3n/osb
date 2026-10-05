package deb

import (
	"archive/tar"
	"fmt"

	"pault.ag/go/debian/deb"
)

type Deb struct {
	Control Control
	Data    *tar.Reader

	close func() error
}

func (d *Deb) Close() error {
	if d == nil || d.close == nil {
		return nil
	}
	return d.close()
}

func ReadDeb(path string) (*Deb, error) {
	debFile, closer, err := deb.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("deb: read %s: %w", path, err)
	}
	c := debFile.Control
	rel := func(field string) string { return c.Values[field] }
	out := &Deb{
		Control: Control{
			Package:       c.Package,
			Source:        c.Source,
			Version:       c.Version.String(),
			Architecture:  c.Architecture.String(),
			Maintainer:    c.Maintainer,
			Description:   c.Description,
			Section:       c.Section,
			Priority:      c.Priority,
			InstalledSize: c.InstalledSize,
			MultiArch:     c.MultiArch,
			Homepage:      c.Homepage,
			Depends:       rel("Depends"),
			PreDepends:    rel("Pre-Depends"),
			Recommends:    rel("Recommends"),
			Suggests:      rel("Suggests"),
			Enhances:      rel("Enhances"),
			Conflicts:     rel("Conflicts"),
			Breaks:        rel("Breaks"),
			Replaces:      rel("Replaces"),
			Provides:      rel("Provides"),
		},
		Data:  debFile.Data,
		close: closer,
	}
	return out, nil
}
