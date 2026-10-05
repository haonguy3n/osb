# Read-only root: the real filesystem is mounted read-only and a tmpfs overlay
# carries the writes, so nothing persists across a reboot.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "readonly-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    features = ["readonly"],
)
