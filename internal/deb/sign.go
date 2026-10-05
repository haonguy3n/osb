package deb

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

func SignInRelease(releaseBytes []byte, homedir, keyID string) ([]byte, error) {
	if _, err := exec.LookPath("gpg"); err != nil {
		return nil, fmt.Errorf("deb sign: gpg missing on PATH: %w", err)
	}
	if homedir == "" {
		return nil, fmt.Errorf("deb sign: homedir required (no ambient GNUPGHOME fallback)")
	}
	if _, err := os.Stat(homedir); err != nil {
		return nil, fmt.Errorf("deb sign: homedir %s: %w", homedir, err)
	}

	args := []string{
		"--batch",
		"--pinentry-mode", "loopback",
		"--homedir", homedir,
		"--armor",
		"--clearsign",
	}
	if keyID != "" {
		args = append(args, "--local-user", keyID)
	}
	cmd := exec.Command("gpg", args...)
	cmd.Stdin = bytes.NewReader(releaseBytes)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("deb sign: gpg --clearsign: %w: %s", err, errBuf.String())
	}
	return out.Bytes(), nil
}
