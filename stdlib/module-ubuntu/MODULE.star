module_info(
    name = "ubuntu",
    description = "Wraps Ubuntu's package feeds as osb units, and ships an Ubuntu/glibc-side build toolchain (toolchain-ubuntu-26.04). Ubuntu shares Debian's apt/dpkg repository format, so it uses the same apt_feed() builtin with distro = \"ubuntu\". The Ubuntu release pinned below MUST match the FROM ubuntu:<release> in containers/toolchain-ubuntu-26.04/Dockerfile — packages from these feeds are ABI- and signing-key-coupled to the toolchain libc.",
)

# Ubuntu shares Debian's apt/dpkg repository format, so it is wrapped
# with the same apt_feed() builtin rather than a bespoke one. Each
# call registers a synthetic module named "<parent>.<component>", so
# consumers reference packages via "ubuntu.main" in prefer_modules. The
# suite kwarg is feed configuration (it picks which on-disk Packages
# file is parsed); only one suite per distro per project is supported,
# so it does not appear in the module identity.
#
# Units materialize lazily as the runtime closure references them —
# declaring a feed costs one Starlark call and the checked-in Packages
# text per arch, not tens of thousands of .star files. The distro =
# "ubuntu" kwarg is stamped on every unit apt_feed synthesizes; that is
# its own distro in osb's resolver (so Ubuntu and Debian closures don't
# collide), while sharing the apt/dpkg/glibc rootfs-assembly backend
# that osb applies to the whole apt family. Ubuntu images set
# distro = "ubuntu" to match.
#
# SPLIT MIRRORS. Ubuntu serves its architectures from two hosts:
# amd64/i386 live on http://archive.ubuntu.com/ubuntu, while arm64 and
# the other ports arches live on http://ports.ubuntu.com/ubuntu-ports.
# Debian's single mirror serves every arch, so apt_feed defaults to
# one `url`; the optional arch_urls map overrides the base URL per
# osb-canonical arch so a single Ubuntu feed can span both hosts. The
# override applies both to `osb update-feeds` (where each arch's
# Packages.gz is fetched) and to the per-deb download URL at build time.
# The InRelease is fetched once from `url` for signature verification —
# both mirrors ship an InRelease signed by the same archive key.
#
# To refresh the in-tree Packages files from upstream after Ubuntu
# ships a point release or security update, run `osb update-feeds` in
# this module's root. That fetches the feed's InRelease, verifies the
# signature against keys/ubuntu-archive-keyring.gpg, applies the
# fingerprint allow-list to any new key, and atomically rewrites
# feeds/<component>/<arch>/Packages.

_UBUNTU_MIRROR = "http://archive.ubuntu.com/ubuntu"
_UBUNTU_PORTS = "http://ports.ubuntu.com/ubuntu-ports"
_UBUNTU_SUITE = "resolute"

apt_feed(
    name = "main",
    distro = "ubuntu",
    url = _UBUNTU_MIRROR,
    arch_urls = {
        "arm64": _UBUNTU_PORTS,
    },
    suite = _UBUNTU_SUITE,
    component = "main",
    arches = ["amd64", "arm64"],
    index = "feeds/main",
    keyring = "keys/ubuntu-archive-keyring.gpg",
)

apt_feed(
    name = "universe",
    distro = "ubuntu",
    url = _UBUNTU_MIRROR,
    arch_urls = {
        "arm64": _UBUNTU_PORTS,
    },
    suite = _UBUNTU_SUITE,
    component = "universe",
    arches = ["amd64", "arm64"],
    index = "feeds/universe",
    keyring = "keys/ubuntu-archive-keyring.gpg",
)

apt_feed(
    name = "restricted",
    distro = "ubuntu",
    url = _UBUNTU_MIRROR,
    arch_urls = {
        "arm64": _UBUNTU_PORTS,
    },
    suite = _UBUNTU_SUITE,
    component = "restricted",
    arches = ["amd64", "arm64"],
    index = "feeds/restricted",
    keyring = "keys/ubuntu-archive-keyring.gpg",
)

apt_feed(
    name = "multiverse",
    distro = "ubuntu",
    url = _UBUNTU_MIRROR,
    arch_urls = {
        "arm64": _UBUNTU_PORTS,
    },
    suite = _UBUNTU_SUITE,
    component = "multiverse",
    arches = ["amd64", "arm64"],
    index = "feeds/multiverse",
    keyring = "keys/ubuntu-archive-keyring.gpg",
)
