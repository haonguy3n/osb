unit(
    name = "syslinux",
    version = "6.03",
    # kernel.org removed the syslinux mirror directory (404 since mid-2026);
    # zytor.com is syslinux's home site.
    source = "https://www.zytor.com/pub/syslinux/syslinux-6.03.tar.xz",
    license = "GPL-2.0",
    description = "BIOS bootloader (MBR + extlinux, x86 only)",
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps=[
            # syslinux is x86-only — skip on other architectures
            'if [ "$ARCH" != "x86_64" ]; then echo "skipping syslinux on $ARCH"; exit 0; fi',
            "install -D bios/mbr/mbr.bin $DESTDIR/usr/share/syslinux/mbr.bin",
            "install -D bios/mbr/gptmbr.bin $DESTDIR/usr/share/syslinux/gptmbr.bin",
            "install -D bios/com32/elflink/ldlinux/ldlinux.c32 $DESTDIR/boot/extlinux/ldlinux.c32",
            "install -D bios/core/ldlinux.sys $DESTDIR/boot/extlinux/ldlinux.sys",
        ]),
    ],
)
