# The distro's own signed boot chain (features: readonly here, but the loader is
# the point).
#
# bootloader = "shim" puts the distro's shim-signed and grub-efi-amd64-signed on
# the ESP: firmware -> shim (signed by Microsoft's UEFI CA) -> signed GRUB ->
# signed distro kernel, so the image boots under stock Secure Boot keys with no
# key enrolment. osb signs nothing on this path, and verity is refused with it
# because grub.cfg and the initramfs would be unsigned.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "signed-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    bootloader = "shim",
    features = ["readonly"],
)
