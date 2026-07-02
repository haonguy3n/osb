package build

import (
	"strings"
	"testing"
)

// The env is one shared definition consumed by the executor, the
// container shell, and the SDK — these assertions pin the invariants
// that drifted when each surface carried its own copy.
func TestSysrootEnv(t *testing.T) {
	env := SysrootEnv("/build/sysroot", "x86_64")

	for _, key := range []string{"PATH", "PKG_CONFIG_PATH", "CFLAGS", "CPPFLAGS", "LDFLAGS", "LD_LIBRARY_PATH", "PYTHONPATH"} {
		if env[key] == "" {
			t.Errorf("%s missing", key)
		}
	}
	if !strings.HasPrefix(env["PATH"], "/build/sysroot/usr/bin:") {
		t.Errorf("PATH must prefer sysroot binaries: %q", env["PATH"])
	}
	// Debian multiarch paths must be present (inert on alpine).
	tuple := multiarchTuple("x86_64")
	if !strings.Contains(env["CFLAGS"], "/usr/include/"+tuple) {
		t.Errorf("CFLAGS missing multiarch include: %q", env["CFLAGS"])
	}
	if !strings.Contains(env["LDFLAGS"], "/usr/lib/"+tuple) {
		t.Errorf("LDFLAGS missing multiarch libdir: %q", env["LDFLAGS"])
	}
	if !strings.Contains(env["PKG_CONFIG_PATH"], tuple) {
		t.Errorf("PKG_CONFIG_PATH missing multiarch dir: %q", env["PKG_CONFIG_PATH"])
	}

	// The sysroot path must be substituted everywhere, not hardcoded.
	sdk := SysrootEnv("/opt/osb/sysroot", "x86_64")
	for k, v := range sdk {
		if strings.Contains(v, "/build/sysroot") {
			t.Errorf("%s leaks the build sysroot path: %q", k, v)
		}
	}
}
