# module-alpine

Wraps Alpine Linux's `main` and `community` package repos as osb units. Each
repo is declared once as an `alpine_feed(...)` in `MODULE.star`; the thousands
of packages it exposes materialize lazily as a project's runtime closure
references them. The module also ships the `toolchain-musl` build container, a
set of Alpine-based images, and a small hand-curated layer of service-enable
companion units.

The Alpine release pinned in `MODULE.star` and `classes/alpine_pkg.star`
(`v3.21` at the time of writing) MUST match the `FROM alpine:<release>` line in
`@module-core`'s `toolchain-musl` Dockerfile — packages from this module are
ABI- and key-coupled to the toolchain libc. See "Bumping the Alpine release".

## Layout

```
MODULE.star                # alpine_feed() declarations for main + community
classes/
  alpine_pkg.star          # unit class — fetch a prebuilt .apk, verify, extract
feeds/
  main/<arch>/APKINDEX     # checked-in, signature-verified package indexes
  community/<arch>/APKINDEX
keys/                      # Alpine RSA public keys; trust root for the APKINDEX
containers/
  toolchain-musl.star      # Alpine + musl build toolchain (moved here from core)
  toolchain-musl/Dockerfile
images/                    # Alpine-specific images (base, ssh, dev, qt, …)
units/                     # hand-curated *-enable.star service companions only
```

## Feeds: a whole repo as one module entry

`alpine_feed(...)` turns a checked-in directory of APKINDEX files into a
lazily-materialized synthetic module. One call exposes every package in an
upstream Alpine repo; units allocate on demand as an image's runtime closure
references them, so a project pulling 300 packages from a 60k-entry feed pays
for 300 unit allocations, not 60k. Declaring a feed costs one Starlark call and
the checked-in APKINDEX text — not thousands of per-package `.star` files.

`MODULE.star` registers two feeds:

```python
alpine_feed(
    name    = "main",                                # synthetic module: alpine.main
    url     = "https://dl-cdn.alpinelinux.org/alpine",
    branch  = "v3.21",                               # Alpine release tag
    section = "main",                                # repo section
    index   = "feeds/main",                          # dir holding <arch>/APKINDEX
    keys    = _ALPINE_KEYS,
)

alpine_feed(
    name    = "community",                           # synthetic module: alpine.community
    url     = "https://dl-cdn.alpinelinux.org/alpine",
    branch  = "v3.21",
    section = "community",
    index   = "feeds/community",
    keys    = _ALPINE_KEYS,
)
```

The composed module name is `<parent>.<feed-name>` — `alpine.main`,
`alpine.community`. Consumers reference these in `prefer_modules` to pull a
package from the feed. The resolver consults synthetic modules after every real
module, so a from-source override (e.g. `module-core/units/openssl.star`) wins
against the feed automatically by name.

`feeds/main/` and `feeds/community/` mirror Alpine's own feed split — separate
APKINDEX URLs, separate maintenance and security commitments.

## Integrity: the signed APKINDEX is the trust root

The checked-in `feeds/**/APKINDEX` files are RSA-SHA1 signed by Alpine. The
`keys/` directory holds the Alpine public keys, and `alpine_feed(keys=[...])`
declares which keys are trusted. `osb update-feeds` verifies each downloaded
APKINDEX against those keys — pure-Go verification that never consults the
maintainer's `/etc/apk/keys/`, so the module's declared trust list is the one
actually enforced.

Once the APKINDEX is verified, every package's integrity hash (`C:`, an
`apk_checksum`) is lifted verbatim from it — no per-apk download needed to
generate a unit. A materialized feed unit carries:

- `apk_checksum = {arch: "Q1<base64-sha1>="}` — Alpine's own integrity hash from
  the signed APKINDEX. The default; costs zero apk downloads.

Hand-written `alpine_pkg(...)` units (and the class) also accept
`sha256 = {arch: 64-hex}` — osb's standard integrity primitive, computed by
downloading and hashing the actual `.apk` — for cases where the stronger hash is
wanted. `classes/alpine_pkg.star` verifies whichever format the unit declares.

## Maintainer playbook: `osb update-feeds`

When Alpine cuts a new release or ships a security patch, refresh the checked-in
APKINDEX files with one command, run inside this module's root:

```sh
osb update-feeds                    # refresh every feed for every existing arch
osb update-feeds --arch x86_64      # restrict to one arch
osb update-feeds --module-dir ../some/other/module
```

Per `alpine_feed()`, per arch, it:

1. Fetches `<url>/<branch>/<section>/<arch>/APKINDEX.tar.gz` over HTTP.
2. Verifies the RSA-SHA1 signature against the declared `keys=[...]`.
3. Decompresses the inner APKINDEX and atomically writes it to
   `feeds/<section>/<arch>/APKINDEX`.

`osb update-feeds` writes only — it does not stage, commit, or push. The
intended workflow:

```sh
osb update-feeds                # refresh every feed
git diff feeds/                 # spot-check version bumps, new packages, removals
git add feeds/
git commit -m "refresh feeds to Alpine v3.21.x"
git push                        # ships to consumers on next `osb build`
```

> A project that consumes this module keeps its own clone in the project module
> cache, and a `osb build` resets that clone to the pushed upstream state on
> every sync. Always commit and push refreshed feeds upstream — a local-only
> edit in a project's cache is discarded on the next build.

### When the diff looks unexpected

- **Lots of new packages or removals**: confirm the Alpine release moved (a
  point release or branch flip).
- **A signature failure**: either Alpine rotated keys (see below) or the
  download was tampered. The failing key fingerprint is in the error message;
  cross-reference against Alpine's release signing keys before adding a new key.
- **HTTP 404**: the upstream mirror dropped the branch (very old release) or the
  section name in `alpine_feed` is wrong.

### Key rotation

When Alpine rotates its signing key (rare, around major release boundaries),
commit the new public key into `keys/` alongside the old one and add it to
`alpine_feed(keys=[...])`. Both verify during the transition; drop the old key
once every active release the module ships has rotated.

## Service-enable companion units

Alpine ships init scripts (e.g. `docker-openrc`) but leaves them unenabled —
apk's `setup-<pkg>` helpers assume a human runs `rc-update add`, and osb has no
human on the image-assembly path. So the module hand-curates a thin layer of
`*-enable.star` companions under `units/`. Each one depends on the upstream
`-openrc` package and sets `services = [...]`, which bakes the runlevel symlink
into the companion unit's own apk. A project that wants the service running adds
the `*-enable` unit to its image's `artifacts` list.

The current tree carries two:

- **`docker-enable`** → depends on `docker-openrc`, enables `docker` at the
  default runlevel.
- **`navidrome-enable`** → the same pattern for navidrome.

This is the only place hand-written units belong in this module. Do not write
from-source units here, and do not scan the rootfs for init scripts as an enable
mechanism — explicit companions are how a package's services become enabled. The
feeds-as-modules pattern is documented in the osb repo's
`docs/naming-and-resolution.md` ("Feeds as synthetic modules").

## Images

`images/` holds Alpine-specific images that live here rather than in
`module-core` because they pin `distro = "alpine"` and pull Alpine-native
packages:

- **`base-image`** — the minimal bootable Alpine: musl, base-files, busybox,
  linux, apk-tools, openrc, network-config.
- **`ssh-image`** — boots and accepts an SSH login, nothing else; mirrors
  `module-debian`'s ssh-image for apples-to-apples comparison.
- **`dev-image`** — ssh-image plus leaf diagnostic/editor tooling (helix, yazi,
  zellij, htop, strace, curl, …).
- Application images — `bun-image`, `nodejs-image`, `python-image`, `qt-image`,
  `docker-image`, `jukebox-image`, `selfhost-image` — each layering a
  `base-files-<name>` overlay onto the relevant runtime closure.

## The toolchain container

`containers/toolchain-musl` is the Alpine + musl build container every musl unit
builds in. It lives here (not in `module-core`) because its
`FROM alpine:<release>` is the other half of the release-coupling contract
below. The container provides only the minimal bootstrap toolchain (gcc,
binutils, make); everything else is a unit.

## Install scripts and triggers

The osb target ships the same init system Alpine assumes (OpenRC,
adduser/addgroup, `/etc/init.d`), so apk install scripts are kept rather than
stripped. `classes/alpine_pkg.star` extracts:

```
.pre-install   .post-install
.pre-upgrade   .post-upgrade
.pre-deinstall .post-deinstall
.trigger
```

into `/lib/apk/db/scripts/<pkgname>/<script>` on the rootfs (mode 755, leading
`.` removed). The image's first-boot service executes them in dependency order —
out of scope for this module.

`.PKGINFO` and `.SIGN.*` are stripped from the destdir extraction: PKGINFO
duplicates metadata already in the APKINDEX, and Alpine's signature is replaced
— osb re-signs the repacked apk with the project's key (see
`internal/artifact/apk.go`'s `RepackAPK`), while PKGINFO and install scripts
pass through to the on-target install untouched.

## Bumping the Alpine release

Three coupled changes that must land in lockstep across this repo and
`@module-core`:

1. `_ALPINE_RELEASE` in `MODULE.star` and `classes/alpine_pkg.star`.
2. `FROM alpine:<release>` in `@module-core`'s `toolchain-musl` Dockerfile, then
   rebuild the toolchain container so its baked apk-tools keyring matches.
3. Refresh every feed for the new release:

   ```sh
   osb update-feeds
   git diff feeds/        # audit removals — packages gone between releases need a decision
   ```

The coupling is not aesthetic: it ties libc ABI, signing keys, and `so:` library
co-versioning together. Mixing releases produces images that link against one
libc at build time and a different one at install time.

## Caveats

- **Stable releases get ~2 years support.** After EOL a release stops receiving
  updates and eventually moves to `archive.alpinelinux.org`. Plan release bumps
  accordingly.
- **`testing` is never wrapped.** No stability or security guarantees; use a
  from-source unit in `module-core` for those packages instead.
- **The feed only describes the latest `-rN` of each package.** A refreshed
  APKINDEX points at the current revision; the previous apk URL 404s on Alpine's
  live mirror within hours. Keep feeds current with `osb update-feeds`, or stand
  up an internal apk mirror that retains old `-rN` builds you've signed off on.
