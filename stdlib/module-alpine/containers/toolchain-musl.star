load("@core//classes/container.star", "container")

# toolchain-musl is the Alpine/musl-side build toolchain. It lives in
# module-alpine because it is Alpine-side build infrastructure ABI-coupled
# to the Alpine release pinned in this module's MODULE.star (_ALPINE_RELEASE).
#
# provides = ["toolchain"] + distro = "alpine" wire this into osb's
# distro-aware toolchain dispatch (R9 in
# docs/specs/2026-05-25-module-debian.md): classes depend on the virtual
# name "toolchain"; the resolver's provides table finds candidates and the
# per-unit distro compatibility tag (R21a) narrows to the one matching the
# consuming image's effective distro. By construction exactly one
# toolchain is visible per closure — alpine images see this one, debian
# images see toolchain-glibc.
#
# The distro and provides fields are inert on osb core that predates R9's
# landing (unknown kwargs are captured into Unit.Extra and ignored). They
# become load-bearing once internal/starlark/types.go grows the Distro
# field and the closure walker's visibility filter ships.

container(
    name = "toolchain-musl",
    version = "19",
    description = "Alpine-based build toolchain with musl libc, gcc, and essential build tools",
    provides = ["toolchain"],
    distro = "alpine",
)
