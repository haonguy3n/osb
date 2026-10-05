# /data is LUKS2 with the key sealed to the TPM (needs swtpm under QEMU).
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")

image(
    name = "crypt-image",
    packages = BASE_PACKAGES,
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    features = ["tpm", "encrypt"],
)
