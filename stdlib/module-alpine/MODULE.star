module_info(
    name = "alpine",
    description = "Wraps Alpine Linux's main + community package feeds as yoe units. The Alpine release pinned below MUST match the alpine: tag in @module-core's toolchain-musl Dockerfile — packages from these feeds are ABI-coupled to the toolchain libc.",
)

# Each alpine_feed() registers a synthetic module named "<parent>.<feed-name>",
# so consumers reference packages via "alpine.main" / "alpine.community" in
# prefer_modules. Units materialize lazily as the runtime closure references
# them — declaring a feed costs one Starlark call and ~40 MB of checked-in
# APKINDEX text, not 3000+ .star files.
#
# To refresh the in-tree APKINDEX from upstream after Alpine ships a point
# release or security patch, run `yoe update-feeds` in this module's root.
# That fetches each feed's APKINDEX.tar.gz, verifies the signature against
# the keys=[...] list, and atomically rewrites feeds/<section>/<arch>/APKINDEX.
# See docs/module-alpine.md "Maintainer playbook" for the full workflow.

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
