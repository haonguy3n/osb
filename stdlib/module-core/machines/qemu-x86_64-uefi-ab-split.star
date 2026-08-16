# A/B with per-slot boot partitions and persistent data - the layout a device
# needs once the root slots are encrypted.
#
#   ESP     FAT   shared  bootloader
#   BOOT_A  ext4  slot a  kernel + initramfs, must stay readable unencrypted
#   ROOT_A  ext4  slot a  the OS (LUKS covers this, not BOOT)
#   BOOT_B  ext4  slot b
#   ROOT_B  ext4  slot b
#   DATA    ext4  shared  survives an update that replaces both root slots
#
# `role` is what distinguishes these: four of them are ext4, serving three
# different purposes. Without it osb reads every ext4 partition as a root slot
# and calls this a six-slot device. See qemu-x86_64-uefi-ab for the simpler
# ESP + two-root layout that needs no roles.
machine(
    name = "qemu-x86_64-uefi-ab-split",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI, A/B with split boot/root and persistent data",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # No root= - GRUB appends root=LABEL=<slot> for the slot it selects.
        cmdline = "console=ttyS0",
    ),
    distro_packages = {
        "alpine": ["grub", "grub-efi"],
        "debian": ["grub-efi-amd64"],
        "ubuntu": ["grub-efi-amd64"],
    },
    partitions = [
        partition(label = "esp", type = "esp", size = "128M", role = "esp"),
        partition(label = "BOOT_A", type = "ext4", size = "512M", role = "boot", slot = "a"),
        partition(label = "ROOT_A", type = "ext4", size = "2G", role = "root", slot = "a", root = True),
        partition(label = "BOOT_B", type = "ext4", size = "512M", role = "boot", slot = "b"),
        partition(label = "ROOT_B", type = "ext4", size = "2G", role = "root", slot = "b"),
        partition(label = "DATA", type = "ext4", size = "1G", role = "data"),
    ],
    qemu = qemu_config(
        machine = "q35",
        cpu = "host",
        memory = "4G",
        firmware = "ovmf",
        display = "none",
        ports = ["2222:22", "8080:80"],
    ),
)
