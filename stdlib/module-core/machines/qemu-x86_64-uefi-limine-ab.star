machine(
    name = "qemu-x86_64-uefi-limine-ab",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI with A/B dual-slot rootfs (KVM + OVMF + limine)",
    kernel = kernel(
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-amd64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "x86_64_defconfig",
        # No root= here: the generated limine.conf carries one entry per slot
        # and appends root=LABEL=<slot> rauc.slot=<letter> to this base.
        cmdline = "console=ttyS0",
    ),
    bootloader = bootloader(type = "limine"),
    packages = ["limine"],
    # Two ext4 rootfs slots trigger the A/B layout. The build installs the OS
    # into the root=True slot (a) and leaves slot b empty for an on-device
    # update to populate.
    #
    # ROLLBACK DIFFERS FROM THE GRUB A/B MACHINE. limine.conf is static - it
    # has no persistent variables, no boot counter, and no way to fall through
    # to the next entry when one fails - so this machine gives atomic slot
    # *selection* (an updater rewrites `default_entry`, or an operator picks
    # the other entry from the menu) but NOT GRUB's automatic
    # rollback-on-failed-boot. Use qemu-x86_64-uefi-ab when unattended
    # rollback is a requirement. See docs/design/ab-updates.md.
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
