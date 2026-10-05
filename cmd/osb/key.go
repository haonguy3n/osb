package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anhhao17/osb/internal/artifact"
	"github.com/anhhao17/osb/internal/device"
)

func cmdKey(args []string) {
	proj := loadProject("", "")
	if len(args) > 0 && args[0] == "secure-boot" {
		key, cert, err := device.GenerateSecureBootKey(projectDir(), "osb Secure Boot key ("+proj.Name+")")
		fail(err)
		fmt.Printf("Secure Boot key:  %s\nSecure Boot cert: %s\n", key, cert)
		return
	}
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: osb key [secure-boot]")
		os.Exit(2)
	}
	signer, err := artifact.LoadOrGenerateSigner(proj.Name, proj.SigningKey)
	fail(err)
	path := proj.SigningKey
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "osb", "keys", proj.Name+".rsa")
	}
	sum := sha256.Sum256(signer.PubPEM)
	fmt.Printf("Signing key: %s\nPublic key:  %s.pub\nKey name:    %s\nFingerprint: %x\n", path, path, signer.KeyName, sum[:8])
}
