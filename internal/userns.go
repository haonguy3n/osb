package internal

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

var usernsSignatures = []string{
	"setting up uid map: Permission denied",
	"setting up gid map: Permission denied",
}

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

func usernsError(underlying error) error {
	apparmor := readSysctl("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	clone := readSysctl("/proc/sys/kernel/unprivileged_userns_clone")
	return fmt.Errorf("%s\n(underlying: %w)", usernsRemediation(apparmor, clone), underlying)
}

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

func readSysctl(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
