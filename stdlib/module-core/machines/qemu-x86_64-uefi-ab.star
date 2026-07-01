machine(
    name = "qemu-x86_64-uefi-ab",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI with A/B dual-slot rootfs (KVM + OVMF)",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # No root= here: with two rootfs slots GRUB picks the active one from the
        # /EFI/osb/slot marker and appends root=LABEL=<slot> at boot.
        cmdline = "console=ttyS0",
    ),
    distro_packages = {
        "alpine": ["grub", "grub-efi"],
        "debian": ["grub-efi-amd64"],
        "ubuntu": ["grub-efi-amd64"],
    },
    # Two ext4 rootfs slots trigger the A/B layout. The build installs the OS
    # into the root=True slot (a) and leaves slot b empty for an on-device update
    # to populate; booting selects the slot from the ESP marker.
    partitions = [
        partition(label = "esp",      type = "esp",  size = "64M"),
        partition(label = "rootfs-a", type = "ext4", size = "2G", root = True),
        partition(label = "rootfs-b", type = "ext4", size = "2G"),
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
