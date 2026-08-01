# Limine — modern x86 BIOS + UEFI bootloader.
#
# Selected per-machine with `bootloader = bootloader(type = "limine")`. One
# bootloader covers both x86 firmware modes: the same limine.conf drives the
# legacy-BIOS MBR path and the UEFI ESP path, so a machine can switch firmware
# without a second bootloader recipe (syslinux for BIOS, GRUB for UEFI).
#
# WHY THE BINARY RELEASE, NOT THE SOURCE TARBALL
#
# Limine's source tarball builds fine offline, but its configure hard-requires
# clang + ld.lld + llvm-objcopy/objdump + nasm — it rejects a GCC toolchain
# outright ("clang invalid, set CC_FOR_TARGET to a valid program"). Adding the
# whole LLVM stack to every osb toolchain container to build one bootloader is
# a poor trade, so this unit consumes upstream's official binary release
# instead. That release ships the freestanding blobs — BOOTX64.EFI and
# limine-bios.sys, which are firmware payloads with no libc linkage and nothing
# arch-specific to re-derive — plus the deployment tool as a single C file.
#
# The host tool IS built from source here (`make` → plain `cc`), because it is
# the one component that links against the target's libc and therefore must
# match the image's distro. That keeps the musl/glibc-sensitive part
# source-built while avoiding an LLVM dependency for the freestanding parts.
#
# The tarball is sha256-pinned against an immutable GitHub release, so the
# fetch is reproducible the same way every other pinned tarball in the stdlib
# is.
#
# INSTALLED LAYOUT
#   /usr/bin/limine                    deployment tool (`limine bios-install`)
#   /usr/share/limine/BOOTX64.EFI      UEFI application, copied to the ESP
#   /usr/share/limine/limine-bios.sys  BIOS stage 2, read off the root fs
#
# image.star's limine paths read all three out of the assembled rootfs, so
# machines that select limine must carry this unit — the bundled limine
# machines list it in distro_packages for every distro (unlike syslinux, whose
# apt counterpart comes from the toolchain container, limine has no distro
# package here and is always the source-built unit).
unit(
    name = "limine",
    version = "12.5.2",
    source = "https://github.com/Limine-Bootloader/Limine/releases/download/v12.5.2/limine-binary.tar.xz",
    sha256 = "5e2d6eb86623fcdcd2a873c9eca7dcafccb34182c17779535fe824fd57b688c5",
    license = "BSD-2-Clause",
    description = "Modern x86 BIOS/UEFI bootloader (limine.conf, MBR + ESP)",
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            # Limine's x86 ports are the only ones this unit installs, and
            # machine() already rejects bootloader(type="limine") on a
            # non-x86_64 machine. Guard anyway so an arch-wide rebuild of the
            # whole catalog on arm64 skips this unit instead of shipping a
            # package whose payload is an x86 bootloader.
            'if [ "$ARCH" != "x86_64" ]; then echo "skipping limine on $ARCH"; exit 0; fi',

            # Build the deployment tool. Upstream's Makefile is .POSIX with
            # CC=cc, so it picks up the toolchain container's compiler and
            # links against the target libc — the reason this half is built
            # rather than shipped prebuilt.
            "make CC=${CC:-cc}",

            "install -D -m0755 limine $DESTDIR/usr/bin/limine",
            "install -D -m0644 BOOTX64.EFI $DESTDIR/usr/share/limine/BOOTX64.EFI",
            "install -D -m0644 limine-bios.sys $DESTDIR/usr/share/limine/limine-bios.sys",
            "install -D -m0644 LICENSE $DESTDIR/usr/share/licenses/limine/LICENSE",
        ]),
    ],
)
