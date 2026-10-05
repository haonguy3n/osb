package device

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed secureboot/db.crt
var sbCert []byte

//go:embed secureboot/db.key
var sbKey []byte

func SecureBootKeyMaterial(projectDir string) (keyPEM, certPEM []byte, isTest bool) {
	key, kerr := os.ReadFile(filepath.Join(projectDir, "keys", "secureboot", "db.key"))
	cert, cerr := os.ReadFile(filepath.Join(projectDir, "keys", "secureboot", "db.crt"))
	if kerr == nil && cerr == nil {
		return key, cert, false
	}
	return sbKey, sbCert, true
}

func GenerateSecureBootKey(projectDir, commonName string) (keyPath, certPath string, err error) {
	dir := filepath.Join(projectDir, "keys", "secureboot")
	keyPath = filepath.Join(dir, "db.key")
	certPath = filepath.Join(dir, "db.crt")
	if _, statErr := os.Stat(keyPath); statErr == nil {
		return "", "", fmt.Errorf("Secure Boot key already exists at %s", keyPath)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}

	if err := writePEM(keyPath, 0o600, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv)); err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, 0o644, "CERTIFICATE", der); err != nil {
		return "", "", err
	}
	return keyPath, certPath, nil
}

func writePEM(path string, mode os.FileMode, blockType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

var secureBootToolHint = map[string]string{
	"virt-fw-vars": "python3-virt-firmware (Debian/Ubuntu), virt-firmware (Fedora/Arch)",
}

func checkSecureBootRunTools() error {
	for _, t := range []string{"virt-fw-vars"} {
		if _, err := exec.LookPath(t); err != nil {
			return fmt.Errorf("Secure Boot under QEMU needs %q on the host PATH - install %s", t, secureBootToolHint[t])
		}
	}
	return nil
}

func EnrollSecureBootVars(out, varsTemplate string, certPEM []byte, bootFilepaths ...string) error {
	dir, err := os.MkdirTemp("", "osb-sb-cert-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	crtPath := filepath.Join(dir, "db.crt")
	if err := os.WriteFile(crtPath, certPEM, 0o600); err != nil {
		return err
	}
	args := []string{
		"--input", varsTemplate,
		"--set-pk", sbOwnerGUID, crtPath,
		"--add-kek", sbOwnerGUID, crtPath,
		"--add-db", sbOwnerGUID, crtPath,
		"--secure-boot",
	}
	for _, p := range bootFilepaths {
		args = append(args, "--append-boot-filepath", p)
	}
	args = append(args, "--output", out)
	if b, err := exec.Command("virt-fw-vars", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("enrolling Secure Boot keys into the UEFI variable store: %w\n%s", err, b)
	}
	return nil
}

const sbOwnerGUID = "a0b1c2d3-e4f5-6789-abcd-ef0123456789"
