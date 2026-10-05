package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectState_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := DetectState(filepath.Join(dir, "does-not-exist"), "")
	if err != nil {
		t.Fatalf("DetectState on missing dir: %v", err)
	}
	if got != StateEmpty {
		t.Errorf("got %q, want %q", got, StateEmpty)
	}
}

func TestDetectState_NoGitDir(t *testing.T) {
	dir := t.TempDir()
	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState on non-git dir: %v", err)
	}
	if got != StateEmpty {
		t.Errorf("got %q, want %q", got, StateEmpty)
	}
}

func TestDetectState_Pin(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StatePin {
		t.Errorf("got %q, want %q", got, StatePin)
	}
}

func TestDetectState_Dev(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StateDev {
		t.Errorf("got %q, want %q", got, StateDev)
	}
}

func TestDetectState_DevMod(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)
	commitFile(t, dir, "extra.c", "// new content\n", "add extra.c")

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StateDevMod {
		t.Errorf("got %q, want %q", got, StateDevMod)
	}
}

func TestDetectState_DevDirty_Modified(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("int main() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StateDevDirty {
		t.Errorf("got %q, want %q", got, StateDevDirty)
	}
}

func TestDetectState_DevDirty_Untracked(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StateDevDirty {
		t.Errorf("got %q, want %q", got, StateDevDirty)
	}
}

func TestDetectState_DevDirtyOverridesDevMod(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)
	commitFile(t, dir, "extra.c", "// new\n", "add extra.c")
	if err := os.WriteFile(filepath.Join(dir, "extra.c"), []byte("// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DetectState(dir, "")
	if err != nil {
		t.Fatalf("DetectState: %v", err)
	}
	if got != StateDevDirty {
		t.Errorf("got %q, want %q (dirty must win over commits-ahead)", got, StateDevDirty)
	}
}

func TestDetectState_NoUpstreamTag(t *testing.T) {
	dir := initRepo(t)
	addOriginRemote(t, dir)

	got, err := DetectState(dir, "")
	if err == nil {
		t.Fatal("expected non-nil error when upstream tag is missing")
	}
	if got != StateDev {
		t.Errorf("got %q, want %q (best-effort fallback)", got, StateDev)
	}
}

func TestDetectState_CachedPinDisambiguatesCleanCheckout(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)

	if got, _ := DetectState(dir, ""); got != StateDev {
		t.Errorf("no cache → got %q, want %q", got, StateDev)
	}
	if got, _ := DetectState(dir, StatePin); got != StatePin {
		t.Errorf("cached pin → got %q, want %q", got, StatePin)
	}
	if got, _ := DetectState(dir, StateDev); got != StateDev {
		t.Errorf("cached dev → got %q, want %q", got, StateDev)
	}
}

func TestDetectState_DirtyBeatsCachedPin(t *testing.T) {
	dir := initRepo(t)
	markUpstream(t, dir)
	addOriginRemote(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := DetectState(dir, StatePin)
	if got != StateDevDirty {
		t.Errorf("got %q, want %q (dirty must win over cached pin)", got, StateDevDirty)
	}
}

func TestIsDev(t *testing.T) {
	dev := []State{StateDev, StateDevMod, StateDevDirty}
	for _, s := range dev {
		if !IsDev(s) {
			t.Errorf("IsDev(%q) = false, want true", s)
		}
	}
	notDev := []State{StateEmpty, StatePin, StateLocal}
	for _, s := range notDev {
		if IsDev(s) {
			t.Errorf("IsDev(%q) = true, want false", s)
		}
	}
}

func TestSrcHashInputs_DirtyEditChangesHash(t *testing.T) {
	dir := initRepo(t)
	addOriginRemote(t, dir)
	markUpstream(t, dir)

	clean := SrcHashInputs(dir, StateDev)
	if clean == "" {
		t.Fatal("SrcHashInputs returned empty for clean dev state")
	}

	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("int main(){return 1;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirty := SrcHashInputs(dir, StateDevDirty)
	if dirty == "" {
		t.Fatal("SrcHashInputs returned empty for dirty dev state")
	}
	if dirty == clean {
		t.Errorf("dirty hash equals clean hash - edits would be cached:\n  clean: %s\n  dirty: %s", clean, dirty)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("int main(){return 2;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty2 := SrcHashInputs(dir, StateDevDirty)
	if dirty2 == dirty {
		t.Errorf("two distinct edits produced the same hash:\n  edit1: %s\n  edit2: %s", dirty, dirty2)
	}
}

func TestSrcHashInputs_StateDevSkipsDirtyDiff(t *testing.T) {
	dir := initRepo(t)
	addOriginRemote(t, dir)
	markUpstream(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asDev := SrcHashInputs(dir, StateDev)
	asDirty := SrcHashInputs(dir, StateDevDirty)
	if asDev == asDirty {
		t.Errorf("state argument must affect output:\n  StateDev:      %s\n  StateDevDirty: %s", asDev, asDirty)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte("int main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-q", "-m", "initial")
	return dir
}

func markUpstream(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "tag", PinTag)
}

func addOriginRemote(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "remote", "add", "origin", "https://example.com/stub.git")
}

func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-q", "-m", msg)
}
