module_info(
    name = "debian",
    description = "Wraps Debian's main + contrib + non-free-firmware + non-free package feeds as osb units, and ships the Debian/glibc-side build toolchain (toolchain-debian-13). All feeds track one suite (security/updates are separate suites and not yet supported). The Debian release pinned below MUST match the FROM debian:<release> in containers/toolchain-debian-13/Dockerfile - packages from these feeds are ABI- and signing-key-coupled to the toolchain libc.",
    prefer_modules = {
        "debian": {
            "util-linux": "debian.main",
            "zstd": "debian.main",
            "kmod": "debian.main",
        },
    },
)


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
