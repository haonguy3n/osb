load("@core//classes/cmake.star", "cmake")

cmake(
    name = "hello-cpp",
    version = "1.0.0",
    source = "./hello-cpp",
    deps = ["libgreet"],
    runtime_deps = ["libgreet"],
    distro_deps = {
        "alpine": ["zlib-dev"],
        "debian": ["zlib1g-dev"],
        "ubuntu": ["zlib1g-dev"],
    },
    distro_runtime_deps = {
        "alpine": ["zlib", "libstdc++"],
        "debian": ["zlib1g", "libstdc++6"],
        "ubuntu": ["zlib1g", "libstdc++6"],
    },
)
