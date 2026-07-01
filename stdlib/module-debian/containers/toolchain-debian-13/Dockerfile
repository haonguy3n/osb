# Yoe build toolchain — Debian/glibc side
#
# Tools-only container for build execution. The yoe CLI runs on the host
# and invokes this container only for build commands that need the
# toolchain (gcc, bwrap, mkfs, dpkg-deb, apt-ftparchive, gpg, etc.). No
# yoe binary inside.
#
# DEBIAN VERSION COUPLING — read before bumping the FROM line below.
#
# This container's Debian release MUST match _DEBIAN_SUITE in this
# module's MODULE.star. Target packages from the apt_feed (libc6,
# openssl, sqlite3, …) are ABI- and signing-key-coupled to the Debian
# release they were built against. Bumping one without the other yields
# a toolchain whose libc/headers don't match the target image's libc —
# a subtle ABI mismatch that compiles fine, links fine, then segfaults
# on first run.
#
# When bumping: update the FROM tag here AND _DEBIAN_SUITE in this
# module's MODULE.star in the same commit, then rebuild the container so
# its baked debian-archive-keyring matches.

FROM debian:trixie-slim

# Avoid interactive prompts during apt operations.
ENV DEBIAN_FRONTEND=noninteractive

# Common bootstrap toolchain + Debian-specific tooling + yoe-side helpers
# (bubblewrap, dosfstools, mtools, e2fsprogs, util-linux ships with base).
#
# mmdebstrap drives image assembly: it resolves the package list against
# the project's local Debian repo and runs apt+dpkg in a single pass to
# unpack and configure the rootfs. eatmydata no-ops fsync/fdatasync during
# that configure pass — isar-proven ~10x speedup, and the image is archived
# wholesale so build-time fsync is wasted work.
RUN apt-get update && \
    apt-get install --no-install-recommends -y \
        ca-certificates \
        bubblewrap \
        git \
        tar \
        gzip \
        bzip2 \
        xz-utils \
        patch \
        coreutils \
        bash \
        make \
        gcc \
        g++ \
        libc6-dev \
        linux-libc-dev \
        autoconf \
        automake \
        libtool \
        pkgconf \
        perl \
        bc \
        flex \
        bison \
        sed \
        gawk \
        cmake \
        dpkg-dev \
        apt-utils \
        debootstrap \
        mmdebstrap \
        gpg \
        gnupg \
        fakeroot \
        eatmydata \
        e2fsprogs \
        dosfstools \
        mtools \
        fdisk \
        mount \
        diffutils \
        findutils && \
    rm -rf /var/lib/apt/lists/*

# Per-language runtimes (python3, nodejs/npm, bun) are NOT installed here.
# The python_venv and nodejs_app / bun_app classes add their runtimes to
# unit.deps so the debs land in the per-unit sysroot at build time —
# matching CLAUDE.md's "no installing packages in the container" rule and
# letting each project pin its own runtime version.

# x86-only packages (bootloader, firmware) — skip on other arches.
# extlinux is the bootloader installer that writes ldlinux.sys into the
# root partition; image.star's _install_syslinux_debian calls it after
# writing mbr.bin from /usr/lib/SYSLINUX/.
RUN if [ "$(uname -m)" = "x86_64" ]; then \
        apt-get update && \
        apt-get install --no-install-recommends -y \
            syslinux \
            syslinux-common \
            extlinux \
            ovmf && \
        rm -rf /var/lib/apt/lists/*; \
    fi

# Create /build for bwrap bind mounts
RUN mkdir -p /build/src /build/destdir

# Create loop device nodes (needed for losetup in --privileged mode)
RUN for i in $(seq 0 7); do mknod -m 0660 /dev/loop$i b 7 $i 2>/dev/null || true; done

WORKDIR /project
