# Two root slots (root-a, root-b) plus the shared /data they both mount.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "ab-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    features = ["ab"],
)
