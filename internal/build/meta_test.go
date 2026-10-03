package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildMeta_SourceStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &BuildMeta{
		Status:      "complete",
		Hash:        "abc123",
		SourceState: "dev",
	}
	if err := WriteMeta(dir, in); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	out := ReadMeta(dir)
	if out == nil {
		t.Fatal("ReadMeta returned nil")
	}
	if out.SourceState != "dev" {
		t.Errorf("SourceState = %q, want %q", out.SourceState, "dev")
	}
}

func TestBuildMeta_OmitsEmptySourceState(t *testing.T) {
	dir := t.TempDir()
	in := &BuildMeta{Status: "complete", Hash: "abc"}
	if err := WriteMeta(dir, in); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "build.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); contains(got, "source_state") {
		t.Errorf("build.json contains source_state field when empty: %s", got)
	}
}

func TestBuildMeta_ReadsLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"status":"complete","hash":"abc","installed_bytes":42}`
	if err := os.WriteFile(filepath.Join(dir, "build.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ReadMeta(dir)
	if out == nil {
		t.Fatal("ReadMeta returned nil for legacy file")
	}
	if out.Hash != "abc" || out.InstalledBytes != 42 {
		t.Errorf("legacy fields not parsed: %+v", out)
	}
	if out.SourceState != "" {
		t.Errorf("SourceState = %q, want empty", out.SourceState)
	}
}

func TestInitBuildMeta_PreservesDevState(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMeta(dir, &BuildMeta{
		SourceState:    "dev-dirty",
		SourceDescribe: "v1.0-3-gabc1234-dirty",
	}); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	got := initBuildMeta(dir, "newhash", time.Now())
	if got.Status != "building" {
		t.Errorf("Status = %q, want building", got.Status)
	}
	if got.Hash != "newhash" {
		t.Errorf("Hash = %q, want newhash", got.Hash)
	}
	if got.SourceState != "dev-dirty" {
		t.Errorf("SourceState lost across build start: got %q, want dev-dirty",
			got.SourceState)
	}
	if got.SourceDescribe != "v1.0-3-gabc1234-dirty" {
		t.Errorf("SourceDescribe lost: %q", got.SourceDescribe)
	}
}

func TestInitBuildMeta_NoPriorMeta(t *testing.T) {
	dir := t.TempDir()
	got := initBuildMeta(dir, "h", time.Now())
	if got.SourceState != "" {
		t.Errorf("SourceState = %q on never-built unit, want empty", got.SourceState)
	}
	if got.Hash != "h" || got.Status != "building" {
		t.Errorf("unexpected fresh meta: %+v", got)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
