module_info(
    name = "ubuntu",
    description = "Wraps Ubuntu's package feeds as osb units, and ships an Ubuntu/glibc-side build toolchain (toolchain-ubuntu-26.04). Ubuntu shares Debian's apt/dpkg repository format, so it uses the same apt_feed() builtin with distro = \"ubuntu\". The Ubuntu release pinned below MUST match the FROM ubuntu:<release> in containers/toolchain-ubuntu-26.04/Dockerfile - packages from these feeds are ABI- and signing-key-coupled to the toolchain libc.",
    prefer_modules = {
        "ubuntu": {
            "util-linux": "ubuntu.main",
            "zstd": "ubuntu.main",
            "kmod": "ubuntu.main",
        },
    },
)


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
