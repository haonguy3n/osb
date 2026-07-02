machine(
    name = "qemu-arm64-uefi-secureboot-verity",
    arch = "arm64",
    description = "QEMU arm64 UEFI Secure Boot with a dm-verity verified read-only root (AAVMF)",
    secure_boot = True,
    verity = True,
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-arm64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "defconfig",
        # No root= or rw here: osb appends the dm-verity table, root=/dev/dm-0,
        # and ro to the signed cmdline at build time. The kernel builds the
        # verified device from that table (dm-mod.create) and mounts it directly,
        # so the image ships no GRUB and no initramfs.
        cmdline = "console=ttyAMA0",
    ),
    partitions = [
        partition(label = "esp",         type = "esp",         size = "64M"),
        partition(label = "rootfs-data", type = "ext4",        size = "512M", root = True),
        partition(label = "rootfs-hash", type = "verity-hash", size = "32M"),
    ],
    qemu = qemu_config(
        machine     = "virt",
        cpu         = "cortex-a57",
        memory      = "4G",
        firmware    = "ovmf",
        secure_boot = True,
        display     = "none",
        ports       = ["2222:22", "8080:80", "8118:8118"],
    ),
)
