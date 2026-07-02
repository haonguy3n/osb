machine(
    name = "qemu-x86_64-uefi-secureboot-ab",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI Secure Boot with A/B dual-slot rootfs (signed UKI per slot)",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # No root= here: osb signs one UKI per slot and appends
        # root=LABEL=rootfs-<slot> rw rauc.slot=<slot> to each signed cmdline,
        # so the slot choice lives in UEFI boot entries (RAUC's efi backend),
        # not in an unsigned bootloader config.
        cmdline = "console=ttyS0",
    ),
    # No GRUB: each slot boots its own signed UKI from /EFI/osb/<slot>.efi.
    # mkinitfs is needed because the root resolves by label, which takes an
    # initramfs; osb embeds it into each signed UKI (see the secureboot
    # machine for why it must be requested explicitly without GRUB).
    distro_packages = {
        "alpine": ["mkinitfs"],
    },
    # Two ext4 rootfs slots trigger the A/B layout. The build installs the OS
    # into the root=True slot (a), leaves slot b empty for an on-device update
    # to populate, and signs a UKI per slot; UEFI BootOrder/BootNext select
    # the slot (efibootmgr / RAUC's efi backend on the device).
    partitions = [
        partition(label = "esp",      type = "esp",  size = "64M"),
        partition(label = "rootfs-a", type = "ext4", size = "2G", root = True),
        partition(label = "rootfs-b", type = "ext4", size = "2G"),
    ],
    qemu = qemu_config(
        machine     = "q35",
        cpu         = "host",
        memory      = "4G",
        firmware    = "ovmf",
        secure_boot = True,
        display     = "none",
        ports       = ["2222:22", "8080:80"],
    ),
)
