machine(
    name = "qemu-x86_64-limine",
    arch = "x86_64",
    description = "QEMU x86_64 legacy-BIOS virtual machine (KVM + limine)",
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
        # Matches the syslinux BIOS machine: a single MBR partition is always
        # /dev/vda1 under QEMU's virtio-blk, and this is the command line the
        # BIOS boot path is known-good with.
        cmdline = "console=ttyS0 root=/dev/vda1 rw",
    ),
    # Selects limine over the layout-inferred default (which for a non-ESP
    # layout is MBR + syslinux). One bootloader serves both firmware modes, so
    # the only difference from qemu-x86_64-uefi-limine is the partitioning.
    bootloader = bootloader(type = "limine"),
    # Distro-neutral, unlike syslinux: the `limine` unit is source-built in
    # module-core and installs the same payload for every distro, so there is
    # no apt/apk split to express via distro_packages. The unit must be in the
    # image for the disk task to find limine-bios.sys and the deployment tool.
    packages = ["limine"],
    partitions = [
        partition(label = "rootfs", type = "ext4", size = "2G", root = True),
    ],
    qemu = qemu_config(
        machine = "q35",
        cpu = "host",
        memory = "4G",
        firmware = "seabios",
        display = "none",
        ports = ["2222:22", "8080:80", "8118:8118"],
    ),
)
