# mdev comes from busybox-mdev-openrc and ships the "dev" service that Alpine's
# hwdrivers and machine-id need; hwdrivers is what loads drivers by modalias.
# Without both, hwdrivers never runs and an image boots with no network device
# at all - on QEMU the virtio_net module never gets loaded, so DHCP and SSH fail.
_RUNLEVELS = {
    "sysinit": ["sysfs", "cgroups", "devfs", "dmesg", "mdev", "hwdrivers"],
    "boot": ["bootmisc", "hostname", "modules", "sysctl", "localmount"],
    "shutdown": ["mount-ro", "killprocs"],
}

def _runlevels():
    cmds = []
    for level, services in _RUNLEVELS.items():
        cmds.append("mkdir -p $DESTDIR/etc/runlevels/" + level)
        for s in services:
            cmds.append("ln -sf /etc/init.d/%s $DESTDIR/etc/runlevels/%s/%s" % (s, level, s))
    return cmds

unit(
    name = "base-files",
    version = "15.0",
    distro = "alpine",
    scope = "machine",
    license = "MIT",
    description = "Alpine filesystem skeleton: dirs, accounts, inittab, OpenRC runlevels",
    distro_runtime_deps = {"alpine": ["openrc", "busybox-mdev-openrc"]},
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            "mkdir -p $DESTDIR/etc/apk/keys $DESTDIR/root $DESTDIR/proc $DESTDIR/sys $DESTDIR/dev" +
            " $DESTDIR/tmp $DESTDIR/run $DESTDIR/var/tmp $DESTDIR/var/log $DESTDIR/var/cache" +
            " $DESTDIR/var/lib $DESTDIR/var/spool",
            "chmod 1777 $DESTDIR/tmp $DESTDIR/var/tmp",
            "chmod 0700 $DESTDIR/root",
            "ln -sf /run $DESTDIR/var/run",
            # The standard Alpine accounts, as shipped by alpine-baselayout-data
            # 3.6.8. Alpine's packages rely on them being present: sshd refuses to
            # start without the sshd privilege separation user ("Privilege
            # separation user sshd does not exist"), and mdev needs the device
            # groups (tty, disk, video, ...) to own the nodes it creates.
            install_file("passwd", "$DESTDIR/etc/passwd"),
            install_file("group", "$DESTDIR/etc/group"),
            install_file("shadow", "$DESTDIR/etc/shadow", mode = 0o640),
            ": > $DESTDIR/etc/fstab",
        ] + _runlevels() + [
            install_template("inittab.tmpl", "$DESTDIR/etc/inittab"),
            install_template("os-release.tmpl", "$DESTDIR/etc/os-release"),
            install_file("repositories", "$DESTDIR/etc/apk/repositories"),
            "cp \"$OSB_KEYS_DIR/$OSB_KEY_NAME\" \"$DESTDIR/etc/apk/keys/$OSB_KEY_NAME\"",
        ]),
    ],
)
