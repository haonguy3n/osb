package deb

import (
	"fmt"
	"io"
	"strings"
)

type Control struct {
	Package       string
	Source        string
	Version       string
	Architecture  string
	Maintainer    string
	Description   string
	Section       string
	Priority      string
	InstalledSize int
	MultiArch     string
	Homepage      string

	Depends    string
	PreDepends string
	Recommends string
	Suggests   string
	Enhances   string
	Conflicts  string
	Breaks     string
	Replaces   string
	Provides   string
}

func WriteControl(w io.Writer, c Control) error {
	if c.Package == "" {
		return fmt.Errorf("deb: control: Package field required")
	}
	if c.Version == "" {
		return fmt.Errorf("deb: control: Version field required")
	}
	if c.Architecture == "" {
		return fmt.Errorf("deb: control: Architecture field required")
	}
	if c.Maintainer == "" {
		return fmt.Errorf("deb: control: Maintainer field required")
	}
	if c.Description == "" {
		return fmt.Errorf("deb: control: Description field required")
	}

	var b strings.Builder
	emit := func(key, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s: %s\n", key, value)
	}
	emit("Package", c.Package)
	emit("Source", c.Source)
	emit("Version", c.Version)
	emit("Architecture", c.Architecture)
	emit("Maintainer", c.Maintainer)
	if c.InstalledSize > 0 {
		fmt.Fprintf(&b, "Installed-Size: %d\n", c.InstalledSize)
	}
	emit("Multi-Arch", c.MultiArch)
	emit("Section", c.Section)
	emit("Priority", c.Priority)
	emit("Homepage", c.Homepage)
	emit("Pre-Depends", c.PreDepends)
	emit("Depends", c.Depends)
	emit("Recommends", c.Recommends)
	emit("Suggests", c.Suggests)
	emit("Enhances", c.Enhances)
	emit("Conflicts", c.Conflicts)
	emit("Breaks", c.Breaks)
	emit("Replaces", c.Replaces)
	emit("Provides", c.Provides)
	writeDescription(&b, c.Description)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("deb: write control: %w", err)
	}
	return nil
}

func writeDescription(b *strings.Builder, desc string) {
	lines := strings.Split(strings.TrimRight(desc, "\n"), "\n")
	fmt.Fprintf(b, "Description: %s\n", lines[0])
	for _, line := range lines[1:] {
		if line == "" {
			b.WriteString(" .\n")
		} else {
			fmt.Fprintf(b, " %s\n", line)
		}
	}
}
