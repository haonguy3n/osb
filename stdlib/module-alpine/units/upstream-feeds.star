# upstream-feeds — companion unit that ships a dormant on-device enabler for
# the upstream Alpine feed, plus the upstream signing keys (shipped untrusted,
# under /usr/share/yoe/upstream-keys/, until the enabler is run).
#
# A dev image adds `upstream-feeds` to its artifacts; on the booted device,
# running `yoe-enable-upstream-feeds` opts into the upstream feed HELD BACK via
# an apk repository tag, so a plain `apk add`/`apk upgrade` never reaches it.
# Nothing is configured or trusted until the script runs. Excluding this unit
# from an image (the production default) leaves no script and no keys behind.
#
# Experimentation only; never a production update path. See
# docs/on-device-upstream-feeds.md. The mirror/branch/sections baked into the
# script must stay in sync with the alpine_feed(...) calls in MODULE.star.

unit(
    name = "upstream-feeds",
    # Tag the distro so this unit lands in the alpine bucket rather than the
    # distro-neutral one: module-debian and module-ubuntu each register a
    # unit with this same name, and without a distro tag they would collide
    # (last-module-wins) and a non-alpine variant could resolve into an
    # alpine image. With the tag, an image resolves the unit matching its
    # effective distro, exactly like the feed-materialized packages.
    distro = "alpine",
    version = "0.1.0",
    description = "Dormant on-device enabler for the upstream Alpine feed (experimentation).",
    license = "MIT",
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps = [
            install_file(
                "yoe-enable-upstream-feeds",
                "$DESTDIR/usr/sbin/yoe-enable-upstream-feeds",
                mode = 0o755,
            ),
            install_file(
                "alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub",
                "$DESTDIR/usr/share/yoe/upstream-keys/alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub",
            ),
            install_file(
                "alpine-devel@lists.alpinelinux.org-616ae350.rsa.pub",
                "$DESTDIR/usr/share/yoe/upstream-keys/alpine-devel@lists.alpinelinux.org-616ae350.rsa.pub",
            ),
        ]),
    ],
)
