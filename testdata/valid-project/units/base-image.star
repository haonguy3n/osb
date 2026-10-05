image(
    name = "base-image",
    version = "1.0.0",
    description = "Minimal bootable system",
    packages = ["openssh", "myapp"],
    container = "toolchain-musl",
    container_arch = "target",
    hostname = "yoe",
    timezone = "UTC",
    services = ["sshd", "myapp"],
)
