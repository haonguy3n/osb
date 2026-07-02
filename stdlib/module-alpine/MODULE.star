module_info(
    name = "alpine",
    description = "Wraps Alpine Linux's main + community package feeds as osb units. The Alpine release pinned below MUST match the alpine: tag in @module-core's toolchain-musl Dockerfile — packages from these feeds are ABI-coupled to the toolchain libc.",
    # Default pins for units where module-core's monolithic source build
    # collides with Alpine's split packaging: both would own the same
    # shared-library paths/SONAMEs and apk refuses to install. Pinning
    # routes the lib and all its feed consumers to one coordinated
    # source. Projects inherit these automatically; a project's own
    # prefer_modules entry overrides per unit, and pinning to "" restores
    # default module-priority resolution (i.e. the source-built unit).
    prefer_modules = {
        "alpine": {
            # module-core's xz is static-only, but kmod's depmod needs
            # the shared liblzma.so.5 — Alpine's prebuilt ships it.
            "xz": "alpine.main",
            # Alpine's nodejs (and others) link libzstd.so.1 from
            # Alpine's zstd-libs; module-core's zstd bundles its own.
            "zstd": "alpine.main",
            # module-core's util-linux bundles libblkid/libmount/libuuid;
            # Alpine splits them and eudev/glib/e2fsprogs pull the split
            # packages transitively.
            "util-linux": "alpine.main",
            # Alpine ships libcurl.so.4 as its own package that git and
            # other feed consumers link against.
            "curl": "alpine.main",
            # grub-efi pulls Alpine's mkinitfs -> kmod-libs, a second
            # owner of libkmod.so.2.
            "kmod": "alpine.main",
        },
    },
)

# Each alpine_feed() registers a synthetic module named "<parent>.<feed-name>",
# so consumers reference packages via "alpine.main" / "alpine.community" in
# prefer_modules. Units materialize lazily as the runtime closure references
# them — declaring a feed costs one Starlark call and ~40 MB of checked-in
# APKINDEX text, not 3000+ .star files.
#
# To refresh the in-tree APKINDEX from upstream after Alpine ships a point
# release or security patch, run `osb update-feeds` in this module's root.
# That fetches each feed's APKINDEX.tar.gz, verifies the signature against
# the keys=[...] list, and atomically rewrites feeds/<section>/<arch>/APKINDEX.
# See this module's README.md "Maintainer playbook: `osb update-feeds`" for
# the full workflow.

_ALPINE_MIRROR = "https://dl-cdn.alpinelinux.org/alpine"
_ALPINE_RELEASE = "v3.21"
_ALPINE_KEYS = [
    # Alpine signs each arch's APKINDEX with a separate key — list both
    # so update-feeds accepts whichever the upstream mirror serves.
    "keys/alpine-devel@lists.alpinelinux.org-6165ee59.rsa.pub", # x86_64
    "keys/alpine-devel@lists.alpinelinux.org-616ae350.rsa.pub", # aarch64
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
