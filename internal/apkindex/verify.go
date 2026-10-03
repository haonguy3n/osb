package apkindex

import (
	"archive/tar"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func VerifySignatureBytes(data []byte, trustedKeys []string) error {
	bounds, err := gzipStreamBoundaries(data)
	if err != nil {
		return fmt.Errorf("apkindex verify: %w", err)
	}
	if len(bounds) < 2 {
		return ErrNoSignature
	}

	keyName, signature, err := readSignatureEntry(data[bounds[0][0]:bounds[0][1]])
	if err != nil {
		return fmt.Errorf("apkindex verify: %w", err)
	}
	if keyName == "" {
		return ErrNoSignature
	}

	var matched string
	for _, candidate := range trustedKeys {
		if filepath.Base(candidate) == keyName {
			matched = candidate
			break
		}
	}
	if matched == "" {
		return &UntrustedKeyError{KeyName: keyName, Trusted: trustedKeys}
	}

	pub, err := loadPublicKey(matched)
	if err != nil {
		return fmt.Errorf("apkindex verify: load key %s: %w", matched, err)
	}

	signedStart := bounds[0][1]
	digest := sha1.Sum(data[signedStart:])
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA1, digest[:], signature); err != nil {
		return &SignatureMismatchError{KeyName: keyName, Err: err}
	}
	return nil
}

var ErrNoSignature = errSentinel("apkindex verify: no .SIGN.RSA.* entry in tarball")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

type UntrustedKeyError struct {
	KeyName string
	Trusted []string
}

func (e *UntrustedKeyError) Error() string {
	names := make([]string, len(e.Trusted))
	for i, t := range e.Trusted {
		names[i] = filepath.Base(t)
	}
	if len(names) == 0 {
		return fmt.Sprintf("apkindex verify: tarball signed by %q but trusted-key list is empty", e.KeyName)
	}
	return fmt.Sprintf("apkindex verify: tarball signed by %q which is not in trusted-key list %v", e.KeyName, names)
}

type SignatureMismatchError struct {
	KeyName string
	Err     error
}

func (e *SignatureMismatchError) Error() string {
	return fmt.Sprintf("apkindex verify: signature mismatch (key %q): %v", e.KeyName, e.Err)
}

func (e *SignatureMismatchError) Unwrap() error { return e.Err }

func readSignatureEntry(streamBytes []byte) (string, []byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(streamBytes))
	if err != nil {
		return "", nil, fmt.Errorf("gzip open signature stream: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", nil, nil
		}
		if err != nil {
			return "", nil, fmt.Errorf("tar entry: %w", err)
		}
		const prefix = ".SIGN.RSA."
		if !strings.HasPrefix(hdr.Name, prefix) {
			continue
		}
		sig, err := io.ReadAll(tr)
		if err != nil {
			return "", nil, fmt.Errorf("read signature entry: %w", err)
		}
		return hdr.Name[len(prefix):], sig, nil
	}
}

func loadPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: not a PEM block", path)
	}
	switch block.Type {
	case "PUBLIC KEY":
		k, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		rk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%s: not an RSA key (got %T)", path, k)
		}
		return rk, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("%s: unsupported PEM type %q", path, block.Type)
	}
}

type gzipBound [2]int

func gzipStreamBoundaries(data []byte) ([]gzipBound, error) {
	var out []gzipBound
	pos := 0
	for pos < len(data) {
		if pos+10 > len(data) || data[pos] != 0x1f || data[pos+1] != 0x8b {
			break
		}
		start := pos
		flg := data[pos+3]
		hdrEnd := pos + 10
		if flg&0x04 != 0 {
			if hdrEnd+2 > len(data) {
				return nil, fmt.Errorf("truncated FEXTRA")
			}
			xlen := int(binary.LittleEndian.Uint16(data[hdrEnd : hdrEnd+2]))
			hdrEnd += 2 + xlen
		}
		if flg&0x08 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x10 != 0 {
			for hdrEnd < len(data) && data[hdrEnd] != 0 {
				hdrEnd++
			}
			hdrEnd++
		}
		if flg&0x02 != 0 {
			hdrEnd += 2
		}
		if hdrEnd > len(data) {
			return nil, fmt.Errorf("truncated gzip header")
		}
		br := bytes.NewReader(data[hdrEnd:])
		zr := flate.NewReader(br)
		if _, err := io.Copy(io.Discard, zr); err != nil {
			zr.Close()
			return nil, fmt.Errorf("deflate stream %d: %w", len(out), err)
		}
		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("deflate close stream %d: %w", len(out), err)
		}
		deflateConsumed := (len(data) - hdrEnd) - br.Len()
		end := hdrEnd + deflateConsumed + 8
		if end > len(data) {
			return nil, fmt.Errorf("truncated gzip trailer")
		}
		out = append(out, gzipBound{start, end})
		pos = end
	}
	return out, nil
}
