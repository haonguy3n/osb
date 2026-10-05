# dm-verity behind osb's own signed UKI, with the distro's shim as the first
# stage: firmware -> shim-signed (stock keys already trust it) -> osb's UKI,
# verified against MOK -> verity root whose hash is inside that signed UKI.
#
# That is what makes verity meaningful here: the UKI is kernel + initramfs +
# command line signed as one binary, so the root hash cannot be swapped.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "signed-verity-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    bootloader = "shim",
    features = ["secureboot", "verity"],
)
