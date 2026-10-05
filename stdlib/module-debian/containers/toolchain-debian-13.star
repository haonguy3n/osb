load("@core//classes/container.star", "container")


container(
    name = "toolchain-debian-13",
    version = "2",
    description = "Debian 13 (trixie) build toolchain with glibc, gcc, dpkg-dev, apt-utils, and essential build tools",
    provides = ["toolchain"],
    distro = "debian",
)
