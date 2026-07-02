# Baseline package sets shared by the stock images (base-image, dev-image)
# and exported for projects to extend. This file has no side effects — it
# only defines lists — so it is safe to load() from anywhere.
#
# The intended customization pattern is compose-don't-copy: a project image
# that wants "base-image plus my packages" loads these and appends, so it
# keeps tracking stdlib fixes to the baseline instead of freezing a copy:
#
#   load("@core//classes/image.star", "image")
#   load("@core//classes/baseline.star", "BASE_ARTIFACTS", "BASE_DISTRO_ARTIFACTS")
#
#   image(
#       name = "my-image",
#       artifacts = BASE_ARTIFACTS + ["efitools"],
#       distro_artifacts = BASE_DISTRO_ARTIFACTS,
#   )
#
# Closure resolution dedups names, so appending something a baseline list
# already carries is harmless.

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
