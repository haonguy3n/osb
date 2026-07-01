machine(
    name = "x86_64",
    arch = "x86_64",
    description = "Generic bare-metal x86_64 PC (UEFI + GPT + GRUB EFI)",
    kernel = kernel(
        # Per-distro kernel: the from-source `linux` unit on Alpine, the stock
        # feed kernel meta-package on the apt distros. image() resolves the
        # "linux" provides-name to the entry for the build's effective distro.
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # Real hardware: keep both the VGA/HDMI console (tty0) and a serial
        # console (ttyS0) so the machine is usable with or without a display.
        # root=LABEL= keeps the boot line independent of the disk's device name,
        # which varies across NVMe/SATA/USB on physical machines.
        cmdline = "console=tty0 console=ttyS0,115200 root=LABEL=rootfs rw",
    ),
    # GRUB EFI bootloader, pulled from the distro feed — identical to the QEMU
    # UEFI machine. An `esp` partition triggers the GPT + GRUB EFI disk path, so
    # the resulting image boots on any UEFI PC via EFI/BOOT/BOOTX64.EFI.
    distro_packages = {
        "alpine": ["grub", "grub-efi"],
        "debian": ["grub-efi-amd64"],
        "ubuntu": ["grub-efi-amd64"],
    },
    partitions = [
        partition(label = "esp",    type = "esp",  size = "64M"),
        partition(label = "rootfs", type = "ext4", size = "4G", root = True),
    ],
    # No qemu = qemu_config(...): this is a physical target. Build with
    # `osb build base-image --machine x86_64`, then `osb flash base-image <disk>`.
)
