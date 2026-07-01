# module-ubuntu

Wraps prebuilt Ubuntu packages as yoe units, and ships an Ubuntu/glibc
build toolchain. Ubuntu shares Debian's apt/dpkg/glibc machinery, so this
module uses the same `apt_feed()` builtin as `module-debian` — only with
`distro = "ubuntu"`: units fetch a binary `.deb` from a pinned Ubuntu
release, verify its SHA256 against the upstream-signed `Packages` catalog,
and republish it through yoe's project repo. A unit's "build" is just
extracting the deb's `data.tar` into `$DESTDIR`.

The module currently tracks Ubuntu **Resolute Raccoon (26.04 LTS)**. The
suite pinned in `MODULE.star` (`_UBUNTU_SUITE`) should track the
`FROM ubuntu:<release>` line in `containers/toolchain-glibc/Dockerfile`.

## Ubuntu is its own distro

`apt_feed(distro = "ubuntu", ...)` tags every materialized unit with
`Distro = "ubuntu"`, and the images set `distro = "ubuntu"`. That makes
Ubuntu a first-class distro in yoe's resolver — an Ubuntu image's closure
sees only Ubuntu-tagged units, so a project can declare both
`module-debian` and `module-ubuntu` and the two never collide. Under the
hood Ubuntu rides the shared apt/dpkg/glibc **backend** (mmdebstrap rootfs
assembly, `.deb` packaging, apt on device) that yoe applies to the whole
apt family; only the feed identity, suite, and mirror differ.

## Split mirrors (amd64 + arm64)

Ubuntu serves its architectures from two hosts: `amd64`/`i386` live on
`http://archive.ubuntu.com/ubuntu`, while `arm64` and the other ports
arches live on `http://ports.ubuntu.com/ubuntu-ports`. A single
`apt_feed` spans both via the optional `arch_urls` map, which overrides
the base `url` per architecture — used both when `yoe update-feeds`
fetches each arch's `Packages` and when the build downloads each `.deb`.
The InRelease is fetched once from `url` for signature verification; both
mirrors ship an InRelease signed by the same Ubuntu archive key.

## Layout

```
MODULE.star                # apt_feed(distro="ubuntu", ...) declarations
feeds/
  main/                    # Canonical-supported core
    amd64/Packages         # checked-in catalog snapshot (archive.ubuntu.com)
    arm64/Packages         # checked-in catalog snapshot (ports.ubuntu.com)
  universe/                # community-maintained, the bulk of the archive
    amd64/Packages
    arm64/Packages
  restricted/              # proprietary drivers (Canonical-supported)
    amd64/Packages
    arm64/Packages
  multiverse/              # non-free / legally restricted
    amd64/Packages
    arm64/Packages
keys/
  ubuntu-archive-keyring.gpg   # bootstrap keyring for InRelease verification
  allowed-fingerprints         # fingerprint allow-list for new keys
containers/
  toolchain-glibc.star     # Ubuntu/glibc build toolchain (provides "toolchain")
  toolchain-glibc/Dockerfile
classes/
  kernel.star              # ubuntu_kernel() -> linux-image-generic
images/
  base-image.star          # minimal bootable + SSH image
  ssh-image.star           # boot + SSH, no extra tooling
  dev-image.star           # base + diagnostic/editor userland
```

## Feeds

Each `apt_feed()` call registers a synthetic module named
`ubuntu.<component>`, so consumers reference packages via `ubuntu.main`,
`ubuntu.universe`, `ubuntu.restricted`, or `ubuntu.multiverse` in
`prefer_modules`. Declaring a feed costs one Starlark call and the
checked-in `Packages` text — units materialize lazily as the runtime
closure references them, so working memory tracks closure size, not the
full catalog. This is why all four of Ubuntu's components are declared
even though `universe` alone carries ~66k entries per arch: the catalog
text is just sitting on disk, not loaded into memory.

The four components map to Ubuntu's archive sections: `main` (core,
Canonical-supported), `universe` (community-maintained, the bulk of the
archive), `restricted` (proprietary drivers), and `multiverse` (non-free /
legally restricted). All four live under the same suite and are signed by
the same archive key.

To refresh the in-tree `Packages` files after Ubuntu ships a point release
or security update, run `yoe update-feeds --arch x86_64,arm64` in this
module's root. That fetches each feed's `InRelease`, verifies the
signature against `keys/ubuntu-archive-keyring.gpg`, applies the
fingerprint allow-list to any new key, and atomically rewrites every
`feeds/<component>/<arch>/Packages`.

## Toolchain

`containers/toolchain-ubuntu-26.04` is the Ubuntu/glibc build toolchain. It
declares `provides = ["toolchain"]` and `distro = "ubuntu"`, wiring it
into yoe's distro-aware toolchain dispatch: Ubuntu images resolve the
virtual `toolchain` reference to this container, Debian images resolve it
to `module-debian`'s Debian toolchain, and Alpine images resolve it to
`module-alpine`'s `toolchain-musl`.

The Ubuntu and Debian glibc toolchains are **not** interchangeable, and the
unit name carries the release (`toolchain-ubuntu-26.04`) to keep them apart.
The container image tag is `yoe/<unit-name>:<version>-<arch>`, so two
toolchains sharing a name would share a tag and silently overwrite each
other's image — and apt is not forward-compatible across suites, so an
Ubuntu-resolute rootfs assembled by Debian-trixie's apt crashes reading the
resolute repository metadata. Each release-coupled toolchain therefore gets
its own name and its own image.

## Images

- `base-image` — the smallest closure that boots in QEMU and accepts an
  SSH login: kernel, systemd init, libc, coreutils, bash, dpkg/apt,
  openssh-server, and NetworkManager (plus `nm-manage-ethernet`) for
  wired DHCP.
- `ssh-image` — the same boot + SSH closure with no extra tooling, for an
  apples-to-apples size comparison against `module-alpine`'s `ssh-image`.
- `dev-image` — the base closure plus a diagnostic and editor userland
  (curl, htop, strace, less, file, procps, iproute2, ping, vim-tiny) so
  the device is usable for work over SSH.

The rootfs is assembled with `mmdebstrap --variant=custom`, which
installs exactly the listed closure and its hard dependencies — no
implicit Essential/Priority base. That keeps images minimal but means
the packages dpkg needs at configure time are listed explicitly in each
image (`dash`, `diffutils`, `libc-bin`, `base-files`, `base-passwd`).

## Networking

Ubuntu's `network-manager` ships a drop-in that restricts NetworkManager
to wifi/cellular and delegates wired ethernet to netplan, which yoe images
don't carry — so out of the box the wired NIC stays `unmanaged` and the
image has no network. The images include the `nm-manage-ethernet` unit,
which lays down `/etc/NetworkManager/conf.d/15-yoe-manage-ethernet.conf`
to re-include ethernet in NetworkManager's managed set. The wired NIC then
auto-DHCPs with no connection profile, matching how NetworkManager behaves
by default on Debian.
