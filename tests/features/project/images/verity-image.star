# Feature fixtures for the CI matrix in .github/workflows/features.yml.
#
# Each image turns on one feature combination so a failure names the feature
# rather than the image. They are deliberately separate from the stdlib images
# (base/dev/secure-image) so the tests can be narrowed without changing what
# osb ships to users.
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "verity-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    features = ["secureboot", "verity"],
)
