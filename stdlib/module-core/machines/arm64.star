machine(
    name = "arm64",
    arch = "arm64",
    description = "Generic arm64 board or server with UEFI firmware",
    kernel = {
        "alpine": "linux-lts",
        "debian": "linux-image-arm64",
        "ubuntu": "linux-image-generic",
    },
)
