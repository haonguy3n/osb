module_info(
    name = "alpine",
    description = "Wraps Alpine Linux's main + community package feeds as osb units. The Alpine release pinned below MUST match the alpine: tag in @module-core's toolchain-musl Dockerfile - packages from these feeds are ABI-coupled to the toolchain libc.",
    prefer_modules = {
        "alpine": {
            "xz": "alpine.main",
            "zstd": "alpine.main",
            "util-linux": "alpine.main",
            "curl": "alpine.main",
            "kmod": "alpine.main",
        },
    },
)


_ALPINE_MIRROR = "https://dl-cdn.alpinelinux.org/alpine"
_ALPINE_RELEASE = "v3.21"
_ALPINE_KEYS = [
    "keys/alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub",
    "keys/alpine-devel@lists.alpinelinux.org-616ae350.rsa.pub",
]

alpine_feed(
    name = "main",
    url = _ALPINE_MIRROR,
    branch = _ALPINE_RELEASE,
    section = "main",
    index = "feeds/main",
    keys = _ALPINE_KEYS,
)

alpine_feed(
    name = "community",
    url = _ALPINE_MIRROR,
    branch = _ALPINE_RELEASE,
    section = "community",
    index = "feeds/community",
    keys = _ALPINE_KEYS,
)
