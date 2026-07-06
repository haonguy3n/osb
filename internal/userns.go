package internal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// The build sandbox runs each source compile inside bwrap, which needs an
// unprivileged user namespace with a uid/gid map. When the host forbids that,
// bwrap dies with the opaque line
//
//	bwrap: setting up uid map: Permission denied
//
// deep inside an otherwise-normal build. Ubuntu 23.10+ ship
// kernel.apparmor_restrict_unprivileged_userns=1, which is the usual culprit;
// older Debian/Ubuntu instead gate it behind kernel.unprivileged_userns_clone.
//
// bwrap runs inside the build container, so a host-side namespace probe is not
// a faithful predictor of its outcome (the host may permit namespaces the
// containerized, non-root bwrap process cannot create). Instead we watch
// bwrap's own stderr for the signature above and, when it appears, replace the
// opaque failure with a message naming the exact sysctl to flip.

// usernsSignatures are the bwrap stderr lines that mean "unprivileged user
// namespaces are denied". bwrap emits the uid variant first; the gid variant
// covers kernels that fault on the second map.
var usernsSignatures = []string{
	"setting up uid map: Permission denied",
	"setting up gid map: Permission denied",
}

// usernsWatcher is an io.Writer that passes bytes through to an underlying
// writer while scanning the stream for a bwrap uid/gid-map failure. It tolerates
// the signature being split across Write calls by retaining a short tail.
type usernsWatcher struct {
	w       io.Writer
	tail    []byte
	tripped bool
}

func (d *usernsWatcher) Write(p []byte) (int, error) {
	if !d.tripped {
		hay := append(d.tail, p...)
		for _, sig := range usernsSignatures {
			if bytes.Contains(hay, []byte(sig)) {
				d.tripped = true
				break
			}
		}
		// Retain enough trailing bytes to catch a signature straddling the
		// boundary between this write and the next.
		keep := longestSignatureLen() - 1
		if len(hay) > keep {
			hay = hay[len(hay)-keep:]
		}
		d.tail = append(d.tail[:0], hay...)
	}
	return d.w.Write(p)
}

func longestSignatureLen() int {
	n := 0
	for _, sig := range usernsSignatures {
		if len(sig) > n {
			n = len(sig)
		}
	}
	return n
}

// usernsError wraps the failure from a container command whose stderr showed a
// bwrap uid/gid-map denial, replacing the opaque exit status with actionable
// remediation. The original error is retained (via %w) so callers can still
// inspect the exit status.
func usernsError(underlying error) error {
	apparmor := readSysctl("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	clone := readSysctl("/proc/sys/kernel/unprivileged_userns_clone")
	return fmt.Errorf("%s\n(underlying: %w)", usernsRemediation(apparmor, clone), underlying)
}

// usernsRemediation is the pure message builder, split out so the wording is
// testable without a host that actually restricts namespaces. apparmor and
// clone are the trimmed contents of the two sysctls ("" if absent).
func usernsRemediation(apparmor, clone string) string {
	const preamble = "the build sandbox needs unprivileged user namespaces, but the host denies them\n" +
		"(bwrap failed with \"setting up uid map: Permission denied\")."

	switch {
	case apparmor == "1":
		return preamble + "\n\n" +
			"Ubuntu's AppArmor is restricting them. Allow them with:\n" +
			"  sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0\n" +
			"Persist across reboots:\n" +
			"  echo 'kernel.apparmor_restrict_unprivileged_userns=0' | sudo tee /etc/sysctl.d/60-osb-userns.conf"
	case clone == "0":
		return preamble + "\n\n" +
			"Enable unprivileged user namespaces with:\n" +
			"  sudo sysctl -w kernel.unprivileged_userns_clone=1\n" +
			"Persist across reboots:\n" +
			"  echo 'kernel.unprivileged_userns_clone=1' | sudo tee /etc/sysctl.d/60-osb-userns.conf"
	default:
		return preamble + "\n\n" +
			"Ensure the kernel allows unprivileged user namespaces\n" +
			"(CONFIG_USER_NS=y and user.max_user_namespaces > 0)."
	}
}

// readSysctl returns the trimmed contents of a /proc sysctl file, or "" if it
// can't be read (e.g. the knob doesn't exist on this kernel).
func readSysctl(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
