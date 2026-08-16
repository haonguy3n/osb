load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "ALPINE_BASE", "BASE_ARTIFACTS")

# installer-image - a live image that boots from USB and installs osb onto a
# disk.
#
# This is the interactive counterpart to `osb flash`: flash writes a prebuilt
# image from a build host onto a known device, while this boots on the target
# machine and asks what to do with it. The install itself is osb-installer;
# everything else here exists to give that binary the tools its plan calls.
#
# Build and write to a stick:
#
#   osb build -machine x86_64 installer-image
#   osb flash installer-image /dev/sdX
#
# WHY NO SQUASHFS / OVERLAY. A conventional live image boots a read-only
# squashfs under a tmpfs overlay so the medium is never written. This one boots
# its ext4 root directly, which is simpler and is what osb's existing disk
# layout already produces - the cost is that the stick is mounted read-write, so
# it should be treated as writable media rather than a pristine artifact. The
# installer copies from SourceRoot ("/") with rsync -x, so only the root
# filesystem is transferred and the ESP and pseudo-filesystems are skipped.
#
# The image is Alpine-only. The installer's generated fstab, crypttab, initramfs
# config, and limine paths follow Alpine/mkinitfs conventions (`cryptroot=` /
# `cryptdm=` on the cmdline, /boot/vmlinuz with no version suffix); a
# Debian/Ubuntu installer would need a dracut/initramfs-tools variant of
# configureSteps, which does not exist yet.
image(
    name = "installer-image",
    distro = "alpine",
    artifacts = BASE_ARTIFACTS + [
        "osb-installer",
        # The installer shells out to all of these; osb-installer lists them
        # as runtime_deps too, but naming them here keeps the image readable
        # and survives someone trimming that list.
        "util-linux",
        "e2fsprogs",
        "dosfstools",
        "cryptsetup",
        "rsync",
        "limine",
        # eudev supplies udevadm settle, which the plan uses to wait for
        # partition nodes after sfdisk.
        "eudev",
        "kmod",
        # A shell to fall back to when the install fails and the user needs to
        # look around, plus networking so they can fetch a fix.
        "bash",
        "dhcpcd",
    ] + ALPINE_BASE,
)
