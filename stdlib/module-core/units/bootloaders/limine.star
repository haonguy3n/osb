unit(
    name = "limine",
    version = "12.5.2",
    source = "https://github.com/Limine-Bootloader/Limine/releases/download/v12.5.2/limine-binary.tar.xz",
    sha256 = "5e2d6eb86623fcdcd2a873c9eca7dcafccb34182c17779535fe824fd57b688c5",
    license = "BSD-2-Clause",
    description = "Limine bootloader: x86 BIOS, x86_64 and arm64 UEFI, hybrid ISO",
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            "make CC=${CC:-cc}",
            "install -D -m0755 limine $DESTDIR/usr/bin/limine",
            "install -D -m0644 limine-uefi-cd.bin $DESTDIR/usr/share/limine/limine-uefi-cd.bin",
            "install -D -m0644 LICENSE $DESTDIR/usr/share/licenses/limine/LICENSE",
            'if [ "$ARCH" = arm64 ]; then install -D -m0644 BOOTAA64.EFI $DESTDIR/usr/share/limine/BOOTAA64.EFI; fi',
            'if [ "$ARCH" = x86_64 ]; then for f in BOOTX64.EFI limine-bios.sys limine-bios-cd.bin; do install -D -m0644 $f $DESTDIR/usr/share/limine/$f; done; fi',
        ]),
    ],
)
