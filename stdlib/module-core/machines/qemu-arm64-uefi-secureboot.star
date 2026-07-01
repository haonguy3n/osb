machine(
    name = "qemu-arm64-uefi-secureboot",
    arch = "arm64",
    description = "QEMU arm64 UEFI virtual machine with Secure Boot (AAVMF)",
    secure_boot = True,
    kernel = kernel(
        # Per-distro kernel: the from-source `linux` unit on Alpine, the stock
        # feed kernel meta-package on the apt distros. image() resolves the
        # "linux" provides-name to the entry for the build's effective distro.
        distro_unit = {
            "alpine": "linux",
            "debian": "linux-image-arm64",
            "ubuntu": "linux-image-generic",
        },
        provides = "linux",
        defconfig = "defconfig",
        cmdline = "console=ttyAMA0 root=LABEL=rootfs rw",
    ),
    # Secure Boot boots a signed Unified Kernel Image installed directly on the
    # ESP as /EFI/BOOT/BOOTAA64.EFI; the image ships no GRUB, so no bootloader
    # package is needed. mkinitfs is still required: the image resolves its root
    # by label (root=LABEL=rootfs), which needs an initramfs to find the device,
    # and osb embeds that initramfs into the signed UKI. Without GRUB (which
    # pulls mkinitfs transitively on Alpine) it must be requested explicitly.
    distro_packages = {
        "alpine": ["mkinitfs"],
    },
    partitions = [
        partition(label = "esp",    type = "esp",  size = "64M"),
        partition(label = "rootfs", type = "ext4", size = "2G", root = True),
    ],
    qemu = qemu_config(
        # cortex-a57 (not "host"): the x86 dev host runs this arm64 image under
        # TCG, where a host-passthrough CPU is unavailable.
        machine     = "virt",
        cpu         = "cortex-a57",
        memory      = "4G",
        firmware    = "ovmf",
        secure_boot = True,
        display     = "none",
        ports       = ["2222:22", "8080:80", "8118:8118"],
    ),
)
