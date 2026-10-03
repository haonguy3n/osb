unit(
    name = "nm-manage-ethernet",
    distro = "ubuntu",
    version = "1.0.0",
    license = "MIT",
    description = "NetworkManager drop-in so it manages (auto-DHCPs) wired ethernet on Ubuntu",
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            "mkdir -p $DESTDIR/etc/NetworkManager/conf.d",
            install_file("15-osb-manage-ethernet.conf",
                         "$DESTDIR/etc/NetworkManager/conf.d/15-osb-manage-ethernet.conf",
                         mode = 0o644),
        ]),
    ],
)
