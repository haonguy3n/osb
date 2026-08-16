# Limine - x86 BIOS + UEFI bootloader, selected with
# `bootloader = bootloader(type = "limine")`. One limine.conf drives both
# firmware modes, so a machine can switch firmware without a second recipe.
#
# Uses upstream's binary release: limine's configure hard-requires clang +
# ld.lld + nasm and rejects GCC, and adding LLVM to every toolchain container
# to build one bootloader is a poor trade. The freestanding blobs have no libc
# linkage; only the deployment tool is source-built, because it links against
# the target's libc.
#
# INSTALLED LAYOUT
#   /usr/bin/limine                       deployment tool (`limine bios-install`)
#   /usr/share/limine/BOOTX64.EFI         UEFI application, copied to the ESP
#   /usr/share/limine/limine-bios.sys     BIOS stage 3
#   /usr/share/limine/limine-bios-cd.bin  El Torito BIOS boot image (ISO)
#   /usr/share/limine/limine-uefi-cd.bin  El Torito EFI boot image (ISO)
#
# NOTE: limine 12.5.2 reads only FAT32 and ISO9660 - it has no ext2/ext4
# driver. Anything it must load (stage 3, limine.conf, kernel, initramfs) has
# to live on FAT or an ISO. See docs/testing-with-kvm.md.
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
            # links against the target libc - the reason this half is built
            # rather than shipped prebuilt.
            "make CC=${CC:-cc}",

            "install -D -m0755 limine $DESTDIR/usr/bin/limine",
            "install -D -m0644 BOOTX64.EFI $DESTDIR/usr/share/limine/BOOTX64.EFI",
            "install -D -m0644 limine-bios.sys $DESTDIR/usr/share/limine/limine-bios.sys",
            # El Torito boot images, used only by the ISO path in image.star.
            "install -D -m0644 limine-bios-cd.bin $DESTDIR/usr/share/limine/limine-bios-cd.bin",
            "install -D -m0644 limine-uefi-cd.bin $DESTDIR/usr/share/limine/limine-uefi-cd.bin",
            "install -D -m0644 LICENSE $DESTDIR/usr/share/licenses/limine/LICENSE",
        ]),
    ],
)
