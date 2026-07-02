# The build environment

Unit builds, `osb container shell`, and `osb sdk` all compile against a
**merged dependency sysroot**: every dep's staged output (`sysroot-stage/`,
a hardlink copy of its destdir) merged into one tree. The executor mounts
it at `/build/sysroot`, the SDK bakes it at `/opt/osb/sysroot`. One shared
definition, `build.SysrootEnv(sysroot, arch)`, produces the environment
for all three surfaces so they cannot drift (historically the shell lost
the multiarch and `LD_LIBRARY_PATH` entries the executor grew).

What the env contains, and why:

- **`PATH`** — the sysroot's `usr/bin` first, so dep-provided tools shadow
  the container's own.
- **`CFLAGS` / `CPPFLAGS`** — `-I<sysroot>/usr/include` plus the
  multiarch `-I<sysroot>/usr/include/<tuple>`. Debian puts arch-specific
  headers (e.g. openssl's `opensslconf.h`) under `/usr/include/<tuple>/`;
  Alpine ignores the path since it doesn't exist in its sysroot.
- **`LDFLAGS` / `LD_LIBRARY_PATH`** — `usr/lib` plus the multiarch
  `usr/lib/<tuple>` and `lib/<tuple>` dirs. Debian's multiarch layout puts
  arch-specific libs, `.pc` files, and the core dynamic loader there
  (libc6's `ld-linux`, libssl-dev's libraries).
- **`PKG_CONFIG_PATH`** — the sysroot's pkgconfig dirs (legacy +
  multiarch), then the container's own `/usr/lib` pkgconfig dirs as a
  fallback. The container is target-arch, so those describe
  toolchain-provided libs, never host ones.
- **`PYTHONPATH`** — the sysroot's site-packages, for build tools shipped
  as units (meson).

The executor layers build-context variables on top: `PREFIX`, `DESTDIR`,
`NPROC`, `ARCH`, `MACHINE`, `DISTRO` (the consuming image's effective
distro, so a build-twice source unit can branch on it), `CONSOLE`, `HOME`,
and `REPO` (the local package repository path). The SDK instead adds
`SYSROOT`, `CC`, and `CXX`.
