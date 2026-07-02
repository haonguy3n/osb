package build

import "fmt"

// SysrootEnv returns the compiler/search-path environment for compiling
// against a merged dependency sysroot mounted at the given path. It is
// the single definition shared by the build executor (/build/sysroot),
// `osb container shell` (same), and `osb sdk` (/opt/osb/sysroot) so the
// three surfaces cannot drift — before this existed, the shell had
// already lost the multiarch and LD_LIBRARY_PATH entries the executor
// grew.
//
// Debian's multiarch layout puts arch-specific libs, .pc files, and the
// core dynamic loader under /usr/lib/<tuple>/ and /lib/<tuple>/, and
// arch-specific headers (e.g. openssl's opensslconf.h) under
// /usr/include/<tuple>/. Those paths are included alongside the legacy
// /usr/lib and /usr/include ones so debian-feed deps (liblzma-dev's
// liblzma.pc, libssl-dev's libssl.pc, libc6's ld-linux) resolve during
// builds; Alpine ignores them since they don't exist in its sysroot.
// PKG_CONFIG_PATH also falls back to the container's own /usr/lib
// pkgconfig dirs — the container is target-arch, so those describe
// toolchain-provided libs, not host ones.
func SysrootEnv(sysroot, arch string) map[string]string {
	tuple := multiarchTuple(arch)
	return map[string]string{
		"PATH":            sysroot + "/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"PKG_CONFIG_PATH": fmt.Sprintf("%[1]s/usr/lib/pkgconfig:%[1]s/usr/lib/%[2]s/pkgconfig:/usr/lib/pkgconfig:/usr/lib/%[2]s/pkgconfig", sysroot, tuple),
		"CFLAGS":          fmt.Sprintf("-I%[1]s/usr/include -I%[1]s/usr/include/%[2]s", sysroot, tuple),
		"CPPFLAGS":        fmt.Sprintf("-I%[1]s/usr/include -I%[1]s/usr/include/%[2]s", sysroot, tuple),
		"LDFLAGS":         fmt.Sprintf("-L%[1]s/usr/lib -L%[1]s/usr/lib/%[2]s -L%[1]s/lib/%[2]s", sysroot, tuple),
		"LD_LIBRARY_PATH": fmt.Sprintf("%[1]s/usr/lib:%[1]s/usr/lib/%[2]s:%[1]s/lib/%[2]s", sysroot, tuple),
		"PYTHONPATH":      sysroot + "/usr/lib/python3.12/site-packages",
	}
}
