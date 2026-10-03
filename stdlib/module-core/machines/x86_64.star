machine(
    name = "x86_64",
    arch = "x86_64",
    description = "Generic x86_64 PC (UEFI)",
    console = "ttyS0",
    cmdline = "console=tty0",
    kernel = {
        "alpine": "linux-lts",
        "debian": "linux-image-amd64",
        "ubuntu": "linux-image-generic",
    },
)
