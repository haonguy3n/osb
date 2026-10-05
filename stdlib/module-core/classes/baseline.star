BASE_PACKAGES = ["linux"]

ALPINE_BASE = [
    "base-files",
    "musl",
    "busybox",
    "busybox-binsh",
    "apk-tools",
    "openrc",
    # fsck.vfat: the ESP is a vfat partition with pass 2 in fstab, so the fsck
    # service needs it on every UEFI image.
    "dosfstools",
    "network-config",
    "openssh",
]

APT_BASE = [
    "systemd-sysv",
    "systemd-resolved",
    "init",
    "libc6",
    "apt",
    "openssh-server",
    "network-manager",
]

BASE_DISTRO_PACKAGES = {
    "alpine": ALPINE_BASE,
    "debian": APT_BASE,
    "ubuntu": APT_BASE + ["nm-manage-ethernet"],
}

BASE_SERVICES = {
    "alpine": ["sshd"],
}
