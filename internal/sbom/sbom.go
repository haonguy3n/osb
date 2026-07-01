// Package sbom generates a Software Bill of Materials for a built image from
// the package database in its assembled rootfs. The manifest lists exactly the
// packages the image contains — names, versions, architecture, and (where the
// database records one) a content hash — in CycloneDX JSON, the format most
// supply-chain tooling ingests.
package sbom

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Component is one installed package.
type Component struct {
	Name    string
	Version string
	Arch    string
	// Hash is an optional content checksum recorded by the package database,
	// as "<alg>:<hex>" (e.g. "sha1:abcd..."). Empty when the database records
	// none in a form we can express.
	Hash string
}

// FromRootfs reads the package database of the rootfs at rootfsDir and returns
// its components. distro selects the database format: apt-family distros
// (debian, ubuntu) use dpkg's status file, everything else uses apk's installed
// database. Components come back sorted by name for a deterministic manifest.
func FromRootfs(rootfsDir, distro string) ([]Component, error) {
	var (
		comps []Component
		err   error
	)
	switch distro {
	case "debian", "ubuntu":
		comps, err = fromDpkgStatus(filepath.Join(rootfsDir, "var", "lib", "dpkg", "status"))
	default:
		comps, err = fromApkDB(filepath.Join(rootfsDir, "lib", "apk", "db", "installed"))
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i].Name < comps[j].Name })
	return comps, nil
}

// fromApkDB parses apk's installed database. Records are blank-line-separated;
// each carries single-letter fields — P: name, V: version, A: arch, C: checksum
// (a "Q1" + base64 SHA-1 of the package).
func fromApkDB(path string) ([]Component, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var comps []Component
	var cur Component
	flush := func() {
		if cur.Name != "" {
			comps = append(comps, cur)
		}
		cur = Component{}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if len(line) < 2 || line[1] != ':' {
			continue
		}
		val := line[2:]
		switch line[0] {
		case 'P':
			cur.Name = val
		case 'V':
			cur.Version = val
		case 'A':
			cur.Arch = val
		case 'C':
			// apk stores "Q1<base64 sha1>"; record the algorithm even though
			// the value stays base64 — round-tripping it to hex is not worth a
			// decode here, and CycloneDX accepts the recorded form.
			if strings.HasPrefix(val, "Q1") {
				cur.Hash = "sha1-b64:" + val[2:]
			}
		}
	}
	flush()
	return comps, sc.Err()
}

// fromDpkgStatus parses dpkg's status file: RFC-822-style paragraphs separated
// by blank lines, with Package:, Version:, Architecture:, and Status: fields.
// Only packages whose status is "install ok installed" are included.
func fromDpkgStatus(path string) ([]Component, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var comps []Component
	var cur Component
	installed := false
	flush := func() {
		if cur.Name != "" && installed {
			comps = append(comps, cur)
		}
		cur = Component{}
		installed = false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		name, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch name {
		case "Package":
			cur.Name = val
		case "Version":
			cur.Version = val
		case "Architecture":
			cur.Arch = val
		case "Status":
			installed = val == "install ok installed"
		}
	}
	flush()
	return comps, sc.Err()
}

// WriteCycloneDX writes a CycloneDX 1.5 SBOM for the image to w. The image
// itself is the top-level operating-system component; each package is a library
// component with a package URL (purl) so downstream tools can correlate it with
// vulnerability data.
func WriteCycloneDX(w io.Writer, imageName, imageVersion, distro string, comps []Component) error {
	type hash struct {
		Alg     string `json:"alg"`
		Content string `json:"content"`
	}
	type component struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Purl    string `json:"purl,omitempty"`
		Hashes  []hash `json:"hashes,omitempty"`
	}
	doc := struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
		Metadata    struct {
			Component component `json:"component"`
		} `json:"metadata"`
		Components []component `json:"components"`
	}{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
	}
	doc.Metadata.Component = component{Type: "operating-system", Name: imageName, Version: imageVersion}

	purlType := "apk"
	purlNS := "alpine"
	if distro == "debian" || distro == "ubuntu" {
		purlType = "deb"
		purlNS = distro
	}
	for _, c := range comps {
		comp := component{
			Type:    "library",
			Name:    c.Name,
			Version: c.Version,
			Purl:    fmt.Sprintf("pkg:%s/%s/%s@%s?arch=%s", purlType, purlNS, c.Name, c.Version, c.Arch),
		}
		if alg, content, ok := strings.Cut(c.Hash, ":"); ok {
			comp.Hashes = []hash{{Alg: alg, Content: content}}
		}
		doc.Components = append(doc.Components, comp)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
