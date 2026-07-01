load("@core//classes/container.star", "container")

# toolchain-ubuntu-26.04 is the Ubuntu/glibc-side build toolchain. It lives
# in module-ubuntu because it is Ubuntu-side build infrastructure coupled to
# the Ubuntu release pinned in this module's MODULE.star (_UBUNTU_SUITE) and
# the FROM line in this container's Dockerfile.
#
# provides = ["toolchain"] + distro = "ubuntu" wire this into yoe's
# distro-aware toolchain dispatch: classes depend on the virtual name
# "toolchain"; the resolver's provides table finds candidates and the
# per-unit distro tag narrows to the one matching the consuming image's
# effective distro. Ubuntu images (distro = "ubuntu") resolve "toolchain"
# to this container; Debian images resolve it to module-debian's Debian
# toolchain; Alpine images resolve it to module-alpine's toolchain-musl.
#
# WHY THE RELEASE IS IN THE NAME. The container image tag is derived as
# yoe/<unit-name>:<version>-<arch>, so the unit name must be unique across
# every toolchain a project might co-load. Naming this "toolchain-glibc"
# (as module-debian's once did) made both produce the identical tag
# yoe/toolchain-glibc:1-<arch>; whichever built last won the tag, and an
# Ubuntu rootfs would then be assembled by Debian's apt. The two glibc
# toolchains are NOT interchangeable — apt is not forward-compatible across
# suites, so Debian-trixie's apt crashes reading Ubuntu-resolute's
# repository metadata. Encoding the distro and release in the name gives
# each release-coupled toolchain its own image and forces a fresh build
# when the suite bumps.

container(
    name = "toolchain-ubuntu-26.04",
    version = "1",
    description = "Ubuntu 26.04 build toolchain with glibc, gcc, dpkg-dev, apt-utils, mmdebstrap, and essential build tools",
    provides = ["toolchain"],
    distro = "ubuntu",
)
