package device

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSecureBootKeyMaterial checks the project-key-else-test-key cascade and
// that GenerateSecureBootKey writes a usable pair the resolver then prefers.
func TestSecureBootKeyMaterial(t *testing.T) {
	dir := t.TempDir()

	_, _, isTest := SecureBootKeyMaterial(dir)
	if !isTest {
		t.Fatal("expected the embedded test key when no project key exists")
	}

	keyPath, certPath, err := GenerateSecureBootKey(dir, "osb test")
	if err != nil {
		t.Fatalf("GenerateSecureBootKey: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("key not written: %v", err)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert not written: %v", err)
	}

	key, cert, isTest := SecureBootKeyMaterial(dir)
	if isTest {
		t.Error("expected the project key once generated")
	}
	if len(key) == 0 || len(cert) == 0 {
		t.Error("project key/cert bytes are empty")
	}

	if _, _, err := GenerateSecureBootKey(dir, "osb test"); err == nil {
		t.Error("expected an error when a key already exists")
	}

	if got := filepath.Dir(keyPath); got != filepath.Join(dir, "keys", "secureboot") {
		t.Errorf("unexpected key dir %q", got)
	}
}
