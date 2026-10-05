project(
    name = "osb-features",
    version = "0.1.0",
    defaults = defaults(
        machine = "qemu-x86_64",
        image = "verity-image",
        distro = "ubuntu",
    ),
)
