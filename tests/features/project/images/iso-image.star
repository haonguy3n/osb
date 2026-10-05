# The installer ISO that this project's CI builds and installs from.
#
# `iso = True` adds an <image>.iso: a hybrid BIOS/UEFI ISO that boots the image's
# own kernel and initrd and writes the image to a target disk. The cmdline below
# ends up in the ISO's own boot entry, so the installer takes /dev/vda and powers
# off instead of prompting - which is what makes it testable without a human.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "iso-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    iso = True,
    cmdline = "osb.target=/dev/vda",
)
