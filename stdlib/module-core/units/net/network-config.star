unit(
    name = "network-config",
    distro = "alpine",
    version = "1.0.0",
    license = "MIT",
    description = "DHCP networking on eth0 - uses dhcpcd if installed, else busybox udhcpc",
    services = ["network"],
    runtime_deps = ["busybox", "openrc"],
    replaces = ["busybox"],
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            "mkdir -p $DESTDIR/usr/share/udhcpc $DESTDIR/etc/init.d",
            install_file("udhcpc-default.script",
                         "$DESTDIR/usr/share/udhcpc/default.script", mode = 0o755),
            install_file("network", "$DESTDIR/etc/init.d/network", mode = 0o755),
        ]),
    ],
)
