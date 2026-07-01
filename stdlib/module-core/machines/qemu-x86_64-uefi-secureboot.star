machine(
    name = "qemu-x86_64-uefi-secureboot",
    arch = "x86_64",
    description = "QEMU x86_64 UEFI virtual machine with Secure Boot (KVM + OVMF)",
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
        cmdline = "console=ttyS0 root=LABEL=rootfs rw",
    ),
    # Same GRUB EFI bootloader as the plain UEFI machine — Secure Boot does not
    # change how the image is built. yoe re-signs the ESP's BOOTX64.EFI at run
    # time (on a throwaway copy of the disk) with its embedded test key and
    # enrolls the matching certificate into the OVMF variable store, so the
    # image artifact is byte-identical to the non-Secure-Boot UEFI build.
    distro_packages = {
        "alpine": ["grub", "grub-efi"],
        "debian": ["grub-efi-amd64"],
        "ubuntu": ["grub-efi-amd64"],
    },
    partitions = [
        partition(label = "esp",    type = "esp",  size = "64M"),
        partition(label = "rootfs", type = "ext4", size = "2G", root = True),
    ],
    qemu = qemu_config(
        machine     = "q35",
        cpu         = "host",
        memory      = "4G",
        firmware    = "ovmf",
        secure_boot = True,
        display     = "none",
        ports       = ["2222:22", "8080:80", "8118:8118"],
    ),
)
