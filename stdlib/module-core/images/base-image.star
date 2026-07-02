load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_ARTIFACTS", "BASE_DISTRO_ARTIFACTS")

# Minimal bootable image, one definition for every distro. The smallest closure
# that boots in QEMU and accepts an SSH login. The package sets live in
# classes/baseline.star (side-effect-free, so projects can load and extend
# them without copying this image — see the pattern documented there). Only
# the distro_artifacts branch matching the build's effective distro is
# consulted. The kernel is referenced as the virtual name `"linux"` and
# resolved per (machine, distro) by the machine's kernel config; base-files
# is osb's own distro-agnostic unit, so both live in the shared artifacts
# list, not each distro branch.
image(
    name = "base-image",
    artifacts = BASE_ARTIFACTS,
    distro_artifacts = BASE_DISTRO_ARTIFACTS,
)
