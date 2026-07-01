load("@core//classes/container.star", "container")

# toolchain-musl is the Alpine/musl-side build toolchain. It lives in
# module-alpine because it is Alpine-side build infrastructure ABI-coupled
# to the Alpine release pinned in this module's MODULE.star (_ALPINE_RELEASE).
#
# provides = ["toolchain"] + distro = "alpine" wire this into osb's
# distro-aware toolchain dispatch: classes depend on the virtual name
# "toolchain"; the resolver's provides table finds candidates and the
# per-unit distro compatibility tag narrows to the one matching the
# consuming image's effective distro. By construction exactly one
# toolchain is visible per closure — alpine images see this one, debian
# images see toolchain-glibc.

container(
    name = "toolchain-musl",
    version = "19",
    description = "Alpine-based build toolchain with musl libc, gcc, and essential build tools",
    provides = ["toolchain"],
    distro = "alpine",
)
