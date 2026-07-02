# Baseline package sets shared by the stock images and exported for
# projects to extend (compose-don't-copy: load these and append — the
# README "Customizing a project" section shows the pattern). No side
# effects, safe to load() from anywhere; closure resolution dedups names.

# Distro-neutral roots every bootable image needs: the kernel (a virtual
# name resolved per machine/distro) and osb's own base-files.
BASE_ARTIFACTS = ["linux", "base-files"]

# The smallest Alpine closure that boots and accepts a login.
ALPINE_BASE = [
    "musl",
    "busybox",
    "busybox-binsh",
    "apk-tools",
    "openrc",
    "network-config",
]

# The smallest apt-family closure that boots and accepts an SSH login.
# mmdebstrap --variant=custom installs no implicit Essential/Priority base,
# so the dpkg-configure essentials (dash, diffutils, libc-bin, base-passwd)
# are listed explicitly. NetworkManager self-enables via postinst and
# auto-DHCPs unmanaged ethernet.
APT_BASE = [
    "systemd-sysv",
    "systemd-resolved",
    "init",
    "libc6",
    "libc-bin",
    "base-passwd",
    "dash",
    "diffutils",
    "coreutils",
    "bash",
    "dpkg",
    "apt",
    "openssh-server",
    "network-manager",
]

# Ubuntu additionally needs the nm-manage-ethernet drop-in: its NM leaves
# wired ethernet to netplan, which osb images don't carry. See
# docs/specs/2026-06-03-debian-device-networking.md.
BASE_DISTRO_ARTIFACTS = {
    "alpine": ALPINE_BASE,
    "debian": APT_BASE,
    "ubuntu": APT_BASE + ["nm-manage-ethernet"],
}
