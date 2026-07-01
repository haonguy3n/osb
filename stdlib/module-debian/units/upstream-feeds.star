# upstream-feeds — companion unit that ships a dormant on-device enabler for
# the upstream Debian feed, plus the Debian archive keyring (shipped untrusted,
# under /usr/share/osb/upstream-keys/, until the enabler is run).
#
# A dev image adds `upstream-feeds` to its artifacts; on the booted device,
# running `osb-enable-upstream-feeds` opts into the upstream feed HELD BACK via
# apt pin priority 100 (and a per-source signed-by key, so no global trust
# anchor), so a plain `apt upgrade` never reaches it. Nothing is configured or
# trusted until the script runs. Excluding this unit from an image (the
# production default) leaves no script and no keyring behind.
#
# Experimentation only; never a production update path. See
# docs/on-device-upstream-feeds.md. The mirror/suite/component baked into the
# script must stay in sync with the apt_feed(...) calls in MODULE.star.

unit(
    name = "upstream-feeds",
    # Tag the distro so this unit lands in the debian bucket rather than the
    # distro-neutral one: module-alpine and module-ubuntu each register a
    # unit with this same name, and without a distro tag they would collide
    # (last-module-wins) and the wrong variant could resolve into an image.
    # With the tag, an image resolves the unit matching its effective distro,
    # exactly like the feed-materialized packages.
    distro = "debian",
    version = "0.1.0",
    description = "Dormant on-device enabler for the upstream Debian feed (experimentation).",
    license = "MIT",
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            install_file(
                "osb-enable-upstream-feeds",
                "$DESTDIR/usr/sbin/osb-enable-upstream-feeds",
                mode = 0o755,
            ),
            install_file(
                "debian-archive-keyring.gpg",
                "$DESTDIR/usr/share/osb/upstream-keys/debian-archive-keyring.gpg",
            ),
        ]),
    ],
)
