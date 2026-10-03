load("@core//classes/container.star", "container")


container(
    name = "toolchain-ubuntu-26.04",
    version = "2",
    description = "Ubuntu 26.04 build toolchain with glibc, gcc, dpkg-dev, apt-utils, mmdebstrap, and essential build tools",
    provides = ["toolchain"],
    distro = "ubuntu",
)
