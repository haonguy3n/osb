unit(
    name = "rootoverlay",
    version = "1.0.0",
    license = "MIT",
    description = "Writable tmpfs overlays for a dm-verity read-only root (OpenRC sysinit)",
    runtime_deps = ["busybox", "openrc"],
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            "mkdir -p $DESTDIR/etc/init.d $DESTDIR/etc/runlevels/sysinit $DESTDIR/overlay",
            install_file("rootoverlay.initd", "$DESTDIR/etc/init.d/rootoverlay", mode = 0o755),
            # Enable it in sysinit directly — earlier than the `services = [...]`
            # mechanism, which only wires the default runlevel (after bootmisc).
            "ln -sf /etc/init.d/rootoverlay $DESTDIR/etc/runlevels/sysinit/rootoverlay",
        ]),
    ],
)
