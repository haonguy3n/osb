load("@core//classes/container.star", "container")


container(
    name = "toolchain-musl",
    version = "20",
    description = "Alpine-based build toolchain with musl libc, gcc, and essential build tools",
    provides = ["toolchain"],
    distro = "alpine",
)
