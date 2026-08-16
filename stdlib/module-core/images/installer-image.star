load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "ALPINE_BASE", "BASE_ARTIFACTS")

# installer-image - a live medium that installs osb onto a disk, the
# interactive counterpart to `osb flash`. The install itself is osb-installer;
# everything here exists to supply the tools its plan calls.
#
#   osb build -machine x86_64 installer-image
#   osb flash installer-image /dev/sdX      # or burn/dd the .iso
#
# No squashfs/overlay: the .img boots its ext4 root directly, so the stick is
# writable media rather than a pristine artifact. The .iso instead carries the
# whole rootfs in its initramfs and runs from RAM.
#
# Alpine-only: the generated fstab, crypttab, initramfs config and limine paths
# follow Alpine/mkinitfs conventions. A Debian/Ubuntu target needs a
# dracut/initramfs-tools variant of configureSteps, which does not exist.
image(
    name = "installer-image",
    distro = "alpine",
    iso = True,
    artifacts = BASE_ARTIFACTS + [
        "osb-installer",
        # Named here as well as in osb-installer's runtime_deps, so the image
        # stays readable and survives someone trimming that list.
        # sfdisk and wipefs are their own Alpine packages - the `util-linux`
        # apk is PAM modules only and contains neither.
        "sfdisk",
        "wipefs",
        "e2fsprogs",
        "dosfstools",
        "cryptsetup",
        "rsync",
        "limine",
        "mkinitfs",  # regenerates the target initramfs on encrypted installs
        "eudev",     # udevadm settle, after sfdisk
        "kmod",
        # Fallback shell and networking for when an install fails.
        "bash",
        "dhcpcd",
    ] + ALPINE_BASE,
)
