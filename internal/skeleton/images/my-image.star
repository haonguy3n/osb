load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "my-image",
    packages = BASE_PACKAGES + ["hello-cpp"],
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
)
