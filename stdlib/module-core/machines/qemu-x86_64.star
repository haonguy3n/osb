machine(
    name = "qemu-x86_64",
    arch = "x86_64",
    description = "QEMU x86_64 virtual machine (q35, UEFI)",
    console = "ttyS0",
    kernel = {
        "alpine": "linux-virt",
        "debian": "linux-image-amd64",
        "ubuntu": "linux-image-virtual",
    },
    qemu = qemu_config(machine = "q35", cpu = "host", memory = "2G", ports = ["2222:22", "8080:80"]),
)
