load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")
load("@core//classes/users.star", "user")

image(
    name = "dev-image",
    packages = BASE_PACKAGES + [
        "ca-certificates", "curl", "less", "file", "htop", "strace", "iproute2", "e2fsprogs",
    ],
    distro_packages = {
        "alpine": BASE_DISTRO_PACKAGES["alpine"] + ["procps-ng", "vim", "util-linux"],
        "debian": BASE_DISTRO_PACKAGES["debian"] + ["procps", "iputils-ping", "vim-tiny"],
        "ubuntu": BASE_DISTRO_PACKAGES["ubuntu"] + ["procps", "iputils-ping", "vim-tiny"],
    },
    services = BASE_SERVICES,
    users = [
        user("root"),
        user("user", password = "password", groups = ["wheel"]),
    ],
)
