BASE_PACKAGES = ["linux"]

ALPINE_BASE = [
    "base-files",
    "musl",
    "busybox",
    "busybox-binsh",
    "apk-tools",
    "openrc",
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
