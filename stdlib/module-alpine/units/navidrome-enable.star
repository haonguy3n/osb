# navidrome-enable — companion unit that enables navidrome at boot.
#
# Alpine's navidrome-openrc package ships /etc/init.d/navidrome and
# /etc/conf.d/navidrome verbatim but leaves the service unenabled —
# apk's `setup-navidrome` is the human helper that wires it into the
# default runlevel, and yoe has no humans on the image-assembly path.
# A project that wants navidrome running on the booted image adds
# `navidrome-enable` (this unit) to its image's `artifacts` list;
# runtime_deps pulls in `navidrome-openrc` (the init script) and
# `navidrome` (the binary), and `services = [...]` bakes the runlevel
# symlink into this unit's apk.
#
# The pattern (one *-enable.star companion per service the module
# wants to expose at boot) is documented in
# docs/naming-and-resolution.md "Feeds as synthetic modules" and the
# CLAUDE.md "Distro modules ship a feed + a companion enable layer"
# decision.

unit(
    name = "navidrome-enable",
    version = "1.0.0",
    description = "Enables navidrome at boot (default runlevel) on top of navidrome-openrc.",
    license = "MIT",
    # navidrome-openrc ships /etc/init.d/navidrome — needed at build
    # time so materializeServiceSymlinks can verify the symlink target
    # exists, and at runtime so the symlink actually resolves.
    deps = ["navidrome-openrc"],
    runtime_deps = ["navidrome-openrc", "navidrome"],
    services = ["navidrome"],
    container = "toolchain-musl",
    container_arch = "target",
)
