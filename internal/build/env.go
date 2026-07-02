package build

import "fmt"

// SysrootEnv returns the compiler/search-path environment for compiling
// against a merged dependency sysroot at the given mount path — the
// single definition shared by the executor, `osb container shell`, and
// `osb sdk`. The <tuple> paths serve Debian's multiarch layout and are
// inert on Alpine; see docs/build-environment.md for the full rationale.
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
