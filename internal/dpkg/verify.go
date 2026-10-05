package dpkg

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

func VerifyInRelease(release, keyring []byte) ([]byte, error) {
	block, _ := clearsign.Decode(release)
	if block == nil {
		return nil, ErrNoSignature
	}

	el, err := readKeyring(keyring)
	if err != nil {
		return nil, fmt.Errorf("dpkg verify: keyring: %w", err)
	}

	signer, err := openpgp.CheckDetachedSignature(el, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
	if err != nil {
		fpr := signingFingerprint(block)
		return nil, &UntrustedKeyError{Fingerprint: fpr, Err: err}
	}
	_ = signer

	body := block.Plaintext

	validUntil, hasValidUntil, err := ParseValidUntil(body)
	if err != nil {
		return nil, err
	}
	if hasValidUntil && time.Now().After(validUntil) {
		return nil, &ValidUntilExpiredError{ValidUntil: validUntil, Now: time.Now()}
	}

	return body, nil
}

func ParseValidUntil(body []byte) (t time.Time, ok bool, err error) {
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		s := string(line)
		const prefix = "Valid-Until:"
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		val := strings.TrimSpace(s[len(prefix):])
		layouts := []string{
			"Mon, 02 Jan 2006 15:04:05 MST",
			"Mon, 02 Jan 2006 15:04:05 -0700",
			time.RFC1123,
			time.RFC1123Z,
		}
		for _, layout := range layouts {
			t, err = time.Parse(layout, val)
			if err == nil {
				return t, true, nil
			}
		}
		return time.Time{}, true, fmt.Errorf("dpkg verify: Valid-Until %q: %w", val, err)
	}
	return time.Time{}, false, nil
}

func readKeyring(keyring []byte) (openpgp.EntityList, error) {
	el, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(keyring))
	if err == nil {
		return el, nil
	}
	el, err2 := openpgp.ReadKeyRing(bytes.NewReader(keyring))
	if err2 != nil {
		return nil, fmt.Errorf("not armored: %v; not binary: %w", err, err2)
	}
	return el, nil
}

func signingFingerprint(block *clearsign.Block) string {
	if block == nil || block.ArmoredSignature == nil {
		return ""
	}
	return ""
}

var ErrNoSignature = errSentinel("dpkg verify: input has no PGP clearsigned block")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

type UntrustedKeyError struct {
	Fingerprint string
	Err         error
}

func (e *UntrustedKeyError) Error() string {
	if e.Fingerprint == "" {
		return fmt.Sprintf("dpkg verify: signature did not verify against any key in keyring: %v", e.Err)
	}
	return fmt.Sprintf("dpkg verify: signed by %s but not in trusted keyring: %v", e.Fingerprint, e.Err)
}

func (e *UntrustedKeyError) Unwrap() error { return e.Err }

type ValidUntilExpiredError struct {
	ValidUntil time.Time
	Now        time.Time
}

func (e *ValidUntilExpiredError) Error() string {
	return fmt.Sprintf("dpkg verify: InRelease Valid-Until %s expired (now %s)",
		e.ValidUntil.Format(time.RFC1123), e.Now.Format(time.RFC1123))
}
