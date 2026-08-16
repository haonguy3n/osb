machine(
    name = "qemu-x86_64-uefi-limine",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI virtual machine (KVM + OVMF + limine)",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # root=LABEL= keeps the boot line independent of partition number, and
        # limine addresses the kernel the same way (fslabel(rootfs):/boot/…),
        # so nothing in the boot path hard-codes /dev/vda2.
        cmdline = "console=ttyS0 root=LABEL=rootfs rw",
    ),
    # limine instead of the ESP layout's default GRUB EFI. limine needs no
    # grub-mkimage run and no module directory in the rootfs: the disk task
    # copies the prebuilt BOOTX64.EFI onto the ESP and writes limine.conf
    # beside it, so the image carries no bootloader build tooling.
    bootloader = bootloader(type = "limine"),
    packages = ["limine"],
    partitions = [
        # ESP first: UEFI firmware scans for the EFI System Partition GUID and
        # loads EFI/BOOT/BOOTX64.EFI, which is where limine is installed.
        partition(label = "esp",    type = "esp",  size = "64M"),
        partition(label = "rootfs", type = "ext4", size = "2G", root = True),
    ],
    qemu = qemu_config(
        machine  = "q35",
        cpu      = "host",
        memory   = "4G",
        firmware = "ovmf",
        display  = "none",
        ports    = ["2222:22", "8080:80", "8118:8118"],
    ),
)
