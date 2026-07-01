module_info(
    name = "debian",
    description = "Wraps Debian's main + contrib + non-free-firmware + non-free package feeds as osb units, and ships the Debian/glibc-side build toolchain (toolchain-debian-13). All feeds track one suite (security/updates are separate suites and not yet supported). The Debian release pinned below MUST match the FROM debian:<release> in containers/toolchain-debian-13/Dockerfile — packages from these feeds are ABI- and signing-key-coupled to the toolchain libc.",
)

# Each apt_feed() registers a synthetic module named
# "<parent>.<component>", so consumers reference packages via
# "debian.main" / "debian.contrib" in prefer_modules. The suite kwarg
# is feed configuration (it picks which on-disk Packages file is
# parsed); only one Debian suite per project is supported, so it
# doesn't appear in the module identity.
# Units materialize lazily as the runtime closure references them —
# declaring a feed costs one Starlark call and ~12 MB of checked-in
# Packages text per arch, not 60k+ .star files.
#
# To refresh the in-tree Packages files from upstream after Debian
# ships a point release or security update, run `osb update-feeds` in
# this module's root. That fetches each feed's InRelease, verifies the
# signature against the keys/debian-archive-keyring.gpg list, applies
# the R25 fingerprint allow-list to any new key, and atomically
# rewrites feeds/<component>/<arch>/Packages. See docs/module-debian.md
# (planned) for the full maintainer playbook.

_DEBIAN_MIRROR = "https://deb.debian.org/debian"
_DEBIAN_SUITE = "trixie"

apt_feed(
    name = "main",
    distro = "debian",
    url = _DEBIAN_MIRROR,
    suite = _DEBIAN_SUITE,
    component = "main",
    arches = ["amd64", "arm64"],
    index = "feeds/main",
    keyring = "keys/debian-archive-keyring.gpg",
)

apt_feed(
    name = "contrib",
    distro = "debian",
    url = _DEBIAN_MIRROR,
    suite = _DEBIAN_SUITE,
    component = "contrib",
    arches = ["amd64", "arm64"],
    index = "feeds/contrib",
    keyring = "keys/debian-archive-keyring.gpg",
)

apt_feed(
    name = "non-free-firmware",
    distro = "debian",
    url = _DEBIAN_MIRROR,
    suite = _DEBIAN_SUITE,
    component = "non-free-firmware",
    arches = ["amd64", "arm64"],
    index = "feeds/non-free-firmware",
    keyring = "keys/debian-archive-keyring.gpg",
)

apt_feed(
    name = "non-free",
    distro = "debian",
    url = _DEBIAN_MIRROR,
    suite = _DEBIAN_SUITE,
    component = "non-free",
    arches = ["amd64", "arm64"],
    index = "feeds/non-free",
    keyring = "keys/debian-archive-keyring.gpg",
)
