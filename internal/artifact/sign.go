package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

type Signer struct {
	Key     *rsa.PrivateKey
	KeyName string
	PubPEM  []byte
}

func LoadOrGenerateSigner(projectName, configuredPath string) (*Signer, error) {
	privPath, err := resolveKeyPath(projectName, configuredPath)
	if err != nil {
		return nil, err
	}
	pubPath := privPath + ".pub"

	priv, err := loadOrCreatePrivateKey(privPath)
	if err != nil {
		return nil, err
	}

	pubPEM, err := loadOrWritePublicKey(pubPath, &priv.PublicKey)
	if err != nil {
		return nil, err
	}

	return &Signer{
		Key:     priv,
		KeyName: filepath.Base(pubPath),
		PubPEM:  pubPEM,
	}, nil
}

func resolveKeyPath(projectName, configuredPath string) (string, error) {
	if configuredPath != "" {
		return configuredPath, nil
	}
	if projectName == "" {
		return "", fmt.Errorf("cannot derive default signing key path: project name is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving signing key path: %w (set signing_key on project() to override)", err)
	}
	return filepath.Join(home, ".config", "osb", "keys", projectName+".rsa"), nil
}

func loadOrCreatePrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("parsing %s: not a PEM block", path)
		}
		switch block.Type {
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		case "PRIVATE KEY":
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsing %s: %w", path, err)
			}
			rk, ok := k.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("parsing %s: not an RSA key", path)
			}
			return rk, nil
		default:
			return nil, fmt.Errorf("parsing %s: unexpected PEM type %q", path, block.Type)
		}
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating key dir: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	if err := os.WriteFile(path, pemBytes, 0600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return priv, nil
}

func loadOrWritePublicKey(path string, pub *rsa.PublicKey) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("encoding public key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	})
	if err := os.WriteFile(path, pemBytes, 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return pemBytes, nil
}

func (s *Signer) SignStream(data []byte) ([]byte, error) {
	digest := sha1.Sum(data)
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.Key, crypto.SHA1, digest[:])
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}
	return s.signatureGzipStream(sig)
}

func (s *Signer) signatureGzipStream(signature []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:    ".SIGN.RSA." + s.KeyName,
		Mode:    0644,
		Size:    int64(len(signature)),
		ModTime: SourceDateEpoch(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(signature); err != nil {
		return nil, err
	}
	if err := tw.Flush(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
