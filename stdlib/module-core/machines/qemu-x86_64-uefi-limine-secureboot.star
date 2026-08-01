machine(
    name = "qemu-x86_64-uefi-limine-secureboot",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI Secure Boot via signed limine (KVM + OVMF)",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # This command line is what the build stamps into the enrolled
        # limine.conf. Because the config's hash is baked into the signed EFI
        # binary, the cmdline is as tamper-evident here as it is inside a UKI:
        # editing it on the ESP makes limine panic instead of booting.
        cmdline = "console=ttyS0 root=LABEL=rootfs rw",
    ),
    # Secure Boot through limine rather than a UKI. The build signs limine's
    # BOOTX64.EFI with the project's db key and enrols the blake2b of
    # limine.conf into it; the config in turn pins the kernel and initramfs by
    # blake2b, so the chain runs firmware -> limine -> kernel.
    #
    # Compared with the signed-UKI machines this keeps a real bootloader (a
    # boot menu, serial output, room for more than one entry) at the cost of
    # one more link in the chain. Prefer qemu-x86_64-uefi-secureboot when the
    # smallest possible verified path matters.
    bootloader = bootloader(type = "limine"),
    secure_boot = True,
    packages = ["limine"],
    partitions = [
        partition(label = "esp",    type = "esp",  size = "64M"),
        partition(label = "rootfs", type = "ext4", size = "2G", root = True),
    ],
    qemu = qemu_config(
        machine  = "q35",
        cpu      = "host",
        memory   = "4G",
        firmware = "ovmf",
        display  = "none",
        ports    = ["2222:22", "8080:80"],
    ),
)
