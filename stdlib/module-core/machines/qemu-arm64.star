machine(
    name = "qemu-arm64",
    arch = "arm64",
    description = "QEMU arm64 virtual machine (virt, UEFI)",
    console = "ttyAMA0",
    kernel = {
        "alpine": "linux-virt",
        "debian": "linux-image-arm64",
        "ubuntu": "linux-image-virtual",
    },
    qemu = qemu_config(machine = "virt", cpu = "host", memory = "2G", ports = ["2222:22", "8080:80"]),
)
