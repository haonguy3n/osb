machine(
    name = "qemu-x86_64",
    arch = "x86_64",
    kernel = "linux-qemu",
    console = "ttyS0",
    qemu = qemu_config(machine = "q35", cpu = "host", memory = "1G"),
)
