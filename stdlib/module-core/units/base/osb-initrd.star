unit(
    name = "osb-initrd",
    version = "1.0.0",
    license = "Apache-2.0",
    description = "osb boot initramfs: verity, TPM-sealed LUKS, read-only overlay, grow, installer",
    distro_runtime_deps = {
        "alpine": ["busybox", "kmod"],
        "debian": ["busybox-static", "kmod"],
        "ubuntu": ["busybox-static", "kmod"],
    },
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            install_file("init", "$DESTDIR/usr/lib/osb/init", mode = 0o755),
            install_file("mkinitrd", "$DESTDIR/usr/lib/osb/mkinitrd", mode = 0o755),
            install_file("osb-tpm", "$DESTDIR/usr/bin/osb-tpm", mode = 0o755),
            install_file("osb-dm-trigger.service", "$DESTDIR/usr/lib/systemd/system/osb-dm-trigger.service"),
            "mkdir -p $DESTDIR/usr/lib/systemd/system/sysinit.target.wants",
            "ln -sf ../osb-dm-trigger.service $DESTDIR/usr/lib/systemd/system/sysinit.target.wants/osb-dm-trigger.service",
        ]),
    ],
)
