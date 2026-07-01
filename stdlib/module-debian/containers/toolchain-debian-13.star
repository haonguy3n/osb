load("@core//classes/container.star", "container")

# toolchain-debian-13 is the Debian/glibc-side build toolchain. It lives in
# module-debian because it is Debian-side build infrastructure ABI-coupled
# to the Debian release pinned in this module's MODULE.star (_DEBIAN_SUITE)
# and the FROM line in this container's Dockerfile.
#
# provides = ["toolchain"] + distro = "debian" wire this into osb's
# distro-aware toolchain dispatch: classes depend on the virtual name
# "toolchain"; the resolver's provides table finds candidates and the
# per-unit distro tag narrows to the one matching the consuming image's
# effective distro. Debian images see this toolchain; Ubuntu images see
# module-ubuntu's toolchain-ubuntu-26.04; Alpine images see toolchain-musl.
#
# WHY THE RELEASE IS IN THE NAME. The container image tag is derived as
# osb/<unit-name>:<version>-<arch>, so the unit name must be unique across
# every toolchain a project might co-load. Debian and Ubuntu both build a
# glibc/apt toolchain; naming both "toolchain-glibc" made them produce the
# identical tag and silently overwrite each other's image, so an Ubuntu
# rootfs could be assembled by Debian's apt. The two are NOT
# interchangeable — apt is not forward-compatible across suites — so each
# release-coupled toolchain carries its distro and release in its name.

container(
    name = "toolchain-debian-13",
    version = "1",
    description = "Debian 13 (trixie) build toolchain with glibc, gcc, dpkg-dev, apt-utils, and essential build tools",
    provides = ["toolchain"],
    distro = "debian",
)
