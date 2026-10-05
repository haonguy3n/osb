package resolve

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

var hashSkipFields = map[string]string{
	"Module":            "registration provenance - same unit from different modules must hash identically",
	"ModuleIndex":       "registration order - informational, no output impact",
	"CacheDirs":         "host-side mount points; doesn't affect built artifact contents",
	"ArtifactsExplicit": "UX-only metadata; the resolved Artifacts list (which IS hashed) drives the actual rootfs",
	"Distro":            "visibility-only tag; the consuming image's effective_distro IS hashed (separately) - the per-unit tag has no output impact",
	"PassthroughDeb":    "transport metadata for mirror-verbatim deb publish; the bytes themselves are hashed via SHA256 (which IS in the hash), so this filename doesn't add information",
}

func TestUnitHash_CoversAllFields(t *testing.T) {
	source, err := os.ReadFile("hash.go")
	if err != nil {
		t.Fatalf("reading hash.go: %v", err)
	}
	src := string(source)

	unitType := reflect.TypeFor[osbstar.Unit]()
	for i := 0; i < unitType.NumField(); i++ {
		name := unitType.Field(i).Name
		if _, skipped := hashSkipFields[name]; skipped {
			continue
		}
		if !strings.Contains(src, "unit."+name) {
			t.Errorf("Unit.%s is not referenced in UnitHash; either hash it "+
				"or add it to hashSkipFields with a justification", name)
		}
	}
}

func TestUnitHash_Deterministic(t *testing.T) {
	unit := &osbstar.Unit{
		Name:    "openssh",
		Version: "9.6p1",
		Class:   "package",
		Source:  "https://example.com/openssh.tar.gz",
		SHA256:  "abc123",
		Deps:    []string{"zlib"},
		Tasks:   []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}},
	}

	h1 := UnitHash(unit, "arm64", map[string]string{"zlib": "deadbeef"}, "", "")
	h2 := UnitHash(unit, "arm64", map[string]string{"zlib": "deadbeef"}, "", "")

	if h1 != h2 {
		t.Errorf("hash not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestUnitHash_ChangesOnInput(t *testing.T) {
	unit := &osbstar.Unit{
		Name:    "openssh",
		Version: "9.6p1",
		Class:   "package",
		Source:  "https://example.com/openssh.tar.gz",
		Deps:    []string{"zlib"},
		Tasks:   []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}},
	}

	h1 := UnitHash(unit, "arm64", map[string]string{"zlib": "aaa"}, "", "")

	h2 := UnitHash(unit, "arm64", map[string]string{"zlib": "bbb"}, "", "")
	if h1 == h2 {
		t.Error("hash should change when dependency hash changes")
	}

	h3 := UnitHash(unit, "x86_64", map[string]string{"zlib": "aaa"}, "", "")
	if h1 == h3 {
		t.Error("hash should change when arch changes")
	}

	unit2 := *unit
	unit2.Version = "9.7p1"
	h4 := UnitHash(&unit2, "arm64", map[string]string{"zlib": "aaa"}, "", "")
	if h1 == h4 {
		t.Error("hash should change when version changes")
	}
}

func TestUnitHash_APKChecksumGated(t *testing.T) {
	base := &osbstar.Unit{
		Name:    "thing",
		Version: "1.0",
		Class:   "unit",
		Tasks:   []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "true"}}}},
	}

	h1 := UnitHash(base, "x86_64", nil, "", "")

	withChecksum := *base
	withChecksum.APKChecksum = "Q1wmRLywlDhwD28lS6Qlp6nGlzzIk="
	h2 := UnitHash(&withChecksum, "x86_64", nil, "", "")

	if h1 == h2 {
		t.Error("setting APKChecksum should change the hash")
	}

	other := *base
	other.Description = "different"
	h3 := UnitHash(&other, "x86_64", nil, "", "")
	if h1 == h3 {
		t.Error("description change should still alter the hash")
	}
}

func TestComputeAllHashes(t *testing.T) {
	proj := makeProject(map[string]*osbstar.Unit{
		"zlib":    {Name: "zlib", Version: "1.3", Class: "unit", Deps: nil, Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}}},
		"openssl": {Name: "openssl", Version: "3.0", Class: "unit", Deps: []string{"zlib"}, Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}}},
		"openssh": {Name: "openssh", Version: "9.6", Class: "unit", Deps: []string{"zlib", "openssl"}, Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}}},
	})

	dag, err := BuildDAG(proj, "")
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}

	hashes, err := ComputeAllHashes(dag, "arm64", "", nil, "")
	if err != nil {
		t.Fatalf("ComputeAllHashes: %v", err)
	}

	if len(hashes) != 3 {
		t.Errorf("got %d hashes, want 3", len(hashes))
	}

	if hashes["zlib"] == hashes["openssl"] {
		t.Error("zlib and openssl should have different hashes")
	}
	if hashes["openssl"] == hashes["openssh"] {
		t.Error("openssl and openssh should have different hashes")
	}

	proj.AnyUnit("zlib").Version = "1.4"
	dag2, _ := BuildDAG(proj, "")
	hashes2, _ := ComputeAllHashes(dag2, "arm64", "", nil, "")

	if hashes["zlib"] == hashes2["zlib"] {
		t.Error("zlib hash should change after version bump")
	}
	if hashes["openssh"] == hashes2["openssh"] {
		t.Error("openssh hash should change when transitive dep changes")
	}
}

func TestUnitHash_ExtraAffectsHash(t *testing.T) {
	base := func() *osbstar.Unit {
		return &osbstar.Unit{
			Name:    "my-app",
			Version: "1.0.0",
			Class:   "unit",
			Extra:   map[string]any{"port": int64(8080)},
		}
	}
	h1 := UnitHash(base(), "x86_64", nil, "", "")

	u2 := base()
	u2.Extra["port"] = int64(9000)
	h2 := UnitHash(u2, "x86_64", nil, "", "")

	if h1 == h2 {
		t.Error("hash did not change when Extra changed")
	}
}

func TestUnitHash_ExtraKeyOrderStable(t *testing.T) {
	u1 := &osbstar.Unit{
		Name: "u", Version: "1", Class: "unit",
		Extra: map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)},
	}
	u2 := &osbstar.Unit{
		Name: "u", Version: "1", Class: "unit",
		Extra: map[string]any{"c": int64(3), "b": int64(2), "a": int64(1)},
	}
	if UnitHash(u1, "x86_64", nil, "", "") != UnitHash(u2, "x86_64", nil, "", "") {
		t.Error("hash depends on Extra map iteration order")
	}
}

func TestUnitHash_FilesDirectoryAffectsHash(t *testing.T) {
	tmp := t.TempDir()
	unitDir := filepath.Join(tmp, "unit-src", "u")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "a.tmpl"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	u := &osbstar.Unit{
		Name: "u", Version: "1", Class: "unit",
		DefinedIn: filepath.Join(tmp, "unit-src"),
	}
	h1 := UnitHash(u, "x86_64", nil, "", "")

	if err := os.WriteFile(filepath.Join(unitDir, "a.tmpl"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	h2 := UnitHash(u, "x86_64", nil, "", "")

	if h1 == h2 {
		t.Error("hash did not change when file in unit files dir changed")
	}
}

func TestUnitHash_SrcInputsCacheNeutral(t *testing.T) {
	u := &osbstar.Unit{
		Name: "openssl", Version: "3.4.1", Class: "unit",
		Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}},
	}
	withEmpty := UnitHash(u, "x86_64", nil, "", "")
	withEmpty2 := UnitHash(u, "x86_64", nil, "", "")
	if withEmpty != withEmpty2 {
		t.Fatalf("hash not deterministic with empty srcInputs: %s vs %s", withEmpty, withEmpty2)
	}
}

func TestUnitHash_EffectiveDistroDisambiguates(t *testing.T) {
	u := &osbstar.Unit{
		Name: "openssl", Version: "3.4.1", Class: "unit",
		Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}},
	}
	alpineHash := UnitHash(u, "x86_64", nil, "", "alpine")
	debianHash := UnitHash(u, "x86_64", nil, "", "debian")
	if alpineHash == debianHash {
		t.Error("same source unit must hash differently under alpine vs debian effective distro (R14a)")
	}
	emptyHash := UnitHash(u, "x86_64", nil, "", "")
	if emptyHash == alpineHash {
		t.Error("empty effective distro must differ from alpine (else gate doesn't gate)")
	}
}

func TestComputeAllHashes_EffectiveDistroFlowsThrough(t *testing.T) {
	proj := makeProject(map[string]*osbstar.Unit{
		"zlib": {Name: "zlib", Version: "1.3", Class: "unit", Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}}},
	})
	dag, err := BuildDAG(proj, "")
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	alpineHashes, _ := ComputeAllHashes(dag, "arm64", "", nil, "alpine")
	debianHashes, _ := ComputeAllHashes(dag, "arm64", "", nil, "debian")
	if alpineHashes["zlib"] == debianHashes["zlib"] {
		t.Error("ComputeAllHashes must produce distinct hashes per effective distro for the same source unit")
	}
}

func TestUnitHash_SrcInputsChangesHash(t *testing.T) {
	u := &osbstar.Unit{
		Name: "openssl", Version: "3.4.1", Class: "unit",
		Tasks: []osbstar.Task{{Name: "build", Steps: []osbstar.Step{{Command: "make"}}}},
	}
	pinHash := UnitHash(u, "x86_64", nil, "", "")
	devHash := UnitHash(u, "x86_64", nil, "head:abc123", "")
	if pinHash == devHash {
		t.Error("non-empty srcInputs should change the hash")
	}
	devHash2 := UnitHash(u, "x86_64", nil, "head:abc123", "")
	if devHash != devHash2 {
		t.Error("same srcInputs should produce identical hashes")
	}
	devHashEdited := UnitHash(u, "x86_64", nil, "head:abc123:dirty:deadbeef", "")
	if devHash == devHashEdited {
		t.Error("hash should differ between clean dev and dev-dirty for the same HEAD")
	}
}
