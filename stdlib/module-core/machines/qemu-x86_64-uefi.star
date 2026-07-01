machine(
    name = "qemu-x86_64-uefi",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI virtual machine (KVM + OVMF)",
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
        # Use root=LABEL= so the boot line is independent of partition number.
        # The ESP is partition 1 and the rootfs is partition 2; a /dev/vda2
        # hard-code would break if the partition order ever changes.
        cmdline = "console=ttyS0 root=LABEL=rootfs rw",
    ),
    # GRUB EFI bootloader, pulled from the distro feed.
    #   Alpine: `grub` provides grub-mkimage; `grub-efi` provides the
    #           x86_64-efi module directory at /usr/lib/grub/x86_64-efi/.
    #           Both are required by the image assembly step.
    #   Debian/Ubuntu: `grub-efi-amd64` bundles the EFI modules and pulls
    #           grub-common (which ships grub-mkimage) as a dependency.
    distro_packages = {
        "alpine": ["grub", "grub-efi"],
        "debian": ["grub-efi-amd64"],
        "ubuntu": ["grub-efi-amd64"],
    },
    partitions = [
        # ESP must come first: UEFI firmware scans the disk for the EFI System
        # Partition GUID and loads the default bootloader (EFI/BOOT/BOOTX64.EFI).
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
