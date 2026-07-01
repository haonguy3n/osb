# module-debian

Wraps prebuilt Debian packages as osb units, and ships the Debian/glibc
build toolchain. This is the glibc-side counterpart to `module-alpine`:
where `module-core` builds packages from source, units here fetch a
binary `.deb` from a pinned Debian release, verify its SHA256 against the
upstream-signed `Packages` catalog, and republish it through osb's
project repo. A unit's "build" is just extracting the deb's `data.tar`
into `$DESTDIR`.

The module currently tracks Debian **Trixie**. The suite pinned in
`MODULE.star` (`_DEBIAN_SUITE`) MUST match the `FROM debian:<release>`
line in `containers/toolchain-debian-13/Dockerfile` — packages from these
feeds are ABI- and signing-key-coupled to the toolchain libc.

## Layout

```
MODULE.star                # apt_feed() declarations (one per component)
feeds/
  main/                    # DFSG-free core
    InRelease              # signed release index
    amd64/Packages         # checked-in catalog snapshot
    arm64/Packages
  contrib/                 # free packages that depend on non-free
    amd64/Packages
    arm64/Packages
  non-free-firmware/       # firmware blobs (Wi-Fi, GPU, etc.)
    amd64/Packages
    arm64/Packages
  non-free/                # everything else not DFSG-free
    amd64/Packages
    arm64/Packages
keys/
  debian-archive-keyring.gpg   # bootstrap keyring for InRelease verification
  allowed-fingerprints         # fingerprint allow-list for new keys
containers/
  toolchain-debian-13.star # Debian/glibc build toolchain (provides "toolchain")
  toolchain-debian-13/Dockerfile
images/
  base-image.star          # minimal bootable + SSH image
  ssh-image.star           # boot + SSH, no extra tooling
  dev-image.star           # base + diagnostic/editor userland
```

## Feeds

Each `apt_feed()` in `MODULE.star` registers a synthetic module named
`debian.<component>`, so consumers reference packages via `debian.main`,
`debian.contrib`, `debian.non-free-firmware`, or `debian.non-free` in
`prefer_modules`. Declaring a feed costs one Starlark call and the
checked-in `Packages` text — units materialize lazily as the runtime
closure references them, so working memory tracks closure size, not the
60k+ packages in the catalog.

The four components mirror Debian's archive sections: `main` (DFSG-free
core), `contrib` (free packages that depend on something non-free),
`non-free-firmware` (firmware blobs split out since Debian 12, the
component embedded boards most often need), and `non-free` (everything
else not DFSG-free). All four track the same suite.

**Suites vs. components.** Every feed here is a *component* of the one
pinned suite (`trixie`). Debian's `*-security`, `*-updates`, and
`*-backports` are separate *suites*, not components, and osb currently
supports only one suite per distro: the build toolchain pins a single
release and glibc from a different suite cannot safely mix into the
rootfs. Adding the security pocket is therefore a model change (suite
becomes part of feed identity), not another `apt_feed()` call — between
point releases, refresh the pinned suite with `osb update-feeds` to pull
in fixes that have migrated into it.

To refresh the in-tree `Packages` files after Debian ships a point
release or security update, run `osb update-feeds` in this module's root.
That fetches each feed's `InRelease`, verifies the signature against
`keys/debian-archive-keyring.gpg`, applies the fingerprint allow-list to
any new key, and atomically rewrites every `feeds/<component>/<arch>/Packages`.

## Toolchain

`containers/toolchain-debian-13` is the Debian/glibc build toolchain. It
declares `provides = ["toolchain"]` and `distro = "debian"`, wiring it
into osb's distro-aware toolchain dispatch: Debian images resolve the
virtual `toolchain` reference to this container, Ubuntu images resolve it
to `module-ubuntu`'s `toolchain-ubuntu-26.04`, and Alpine images resolve it
to `module-alpine`'s `toolchain-musl`. It lives here because it is
ABI-coupled to the Debian release pinned in `MODULE.star`.

The Debian and Ubuntu glibc toolchains are **not** interchangeable — apt is
not forward-compatible across suites, so each carries its distro and release
in its unit name. The image tag is `osb/<unit-name>:<version>-<arch>`, so two
toolchains sharing a name would share a tag and overwrite each other's image.

## Images

- `base-image` — the smallest closure that boots in QEMU and accepts an
  SSH login: kernel, systemd init, libc, coreutils, bash, dpkg/apt,
  openssh-server, and NetworkManager for DHCP.
- `ssh-image` — the same boot + SSH closure with no extra tooling, for an
  apples-to-apples size comparison against `module-alpine`'s `ssh-image`.
- `dev-image` — the base closure plus a diagnostic and editor userland
  (curl, htop, strace, less, file, procps, iproute2, ping, vim-tiny) so
  the device is usable for work over SSH.

The rootfs is assembled with `mmdebstrap --variant=custom`, which
installs exactly the listed closure and its hard dependencies — no
implicit Essential/Priority base. That keeps images minimal but means the
packages dpkg needs at configure time are listed explicitly in each
image (`dash`, `diffutils`, `libc-bin`, `base-files`, `base-passwd`).

See [`docs/module-debian.md`](https://github.com/osb/osb/blob/main/docs/module-debian.md)
in the main osb repo for the "when to reach for it" rubric and the full
maintainer playbook.
