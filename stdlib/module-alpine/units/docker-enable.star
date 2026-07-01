# docker-enable — companion unit that enables dockerd at boot.
#
# Alpine ships docker's OpenRC init script in `docker-openrc`, but
# leaves it unenabled — apk's `setup-docker` is a human helper, and yoe
# has no humans on the image-assembly path. A project that wants docker
# running on the booted image adds `docker-enable` (this unit) to its
# image's `artifacts` list; runtime_deps pulls in `docker-openrc`, and
# `services = [...]` bakes the runlevel symlink into this unit's apk.
#
# The pattern (one *-enable.star companion per service the module
# wants to expose at boot) is documented in
# docs/naming-and-resolution.md "Feeds as synthetic modules" and the
# CLAUDE.md "Distro modules ship a feed + a companion enable layer"
# decision.

unit(
    name = "docker-enable",
    version = "0.1.0",
    description = "Enables dockerd at boot (default runlevel) on top of docker-openrc.",
    license = "Apache-2.0",
    # docker-openrc ships /etc/init.d/docker — needed at build time so
    # materializeServiceSymlinks can verify the symlink target exists,
    # and at runtime so the symlink actually resolves.
    deps = ["docker-openrc"],
    runtime_deps = ["docker-openrc"],
    services = ["docker"],
    container = "toolchain-musl",
    container_arch = "target",
)
