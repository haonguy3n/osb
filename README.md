# osb

`osb` builds bootable Linux OS images — Alpine, Debian, or Ubuntu — for x86_64
and arm64 targets, from a single self-contained binary. It bundles its own
standard library (base system, machines, images, and distro package feeds), so a
fresh project builds with **no external repositories to clone**.

- **Single repo, single binary.** The core recipes and distro feeds are embedded
  in the `osb` binary and materialized on first use. `osb init` scaffolds a
  project that builds out of the box.
- **Always-fresh package indexes.** Feed indexes are fetched from the upstream
  mirror on demand rather than shipped as a snapshot, so builds never fail on a
  stale, rotated package.
- **Verified boot.** Any Secure Boot machine builds and boots a signed Unified
  Kernel Image under enforced UEFI Secure Boot in QEMU — no GRUB, no shim.
- **Reproducible, content-addressed builds.** Every unit's output is keyed by its
  inputs; unchanged units are reused from cache.

## Requirements

`osb` orchestrates host tools; it does not bundle them.

- **Go 1.25+** — to build `osb`.
- **Docker** — units build inside containers.
- **QEMU** (`qemu-system-x86_64` / `qemu-system-aarch64`) — for `osb run`.
- **Secure Boot machines only** (secureboot / verity / secureboot-ab):
  `ovmf` (x86_64) or `qemu-efi-aarch64` (arm64), `systemd-ukify`, `mtools`,
  and `python3-virt-firmware`. `osb run` names any missing package before
  launching.
- **Secure Boot via limine** (`*-limine-secureboot`): `sbsign` from
  `sbsigntools` instead of `systemd-ukify` — limine ships a finished EFI
  application that only needs signing, not assembling.

## Build & install

```sh
# Build the binary
make build            # -> ./osb
# or:
go build -o osb ./cmd/osb

# Install onto your PATH
go install ./cmd/osb  # -> $(go env GOPATH)/bin/osb
# or:
sudo install -m755 osb /usr/local/bin/osb

osb version
```

Add `$(go env GOPATH)/bin` to your `PATH` if you used `go install`.

## Quick start

```sh
osb init myproject          # scaffold a project (bundled defaults, no external repos)
cd myproject

osb build base-image                        # Alpine image for the default machine (qemu-x86_64)
osb run  base-image                         # boot it in QEMU (serial console on stdout)
```

Target a different machine or distro:

```sh
osb build -machine qemu-arm64 base-image            # arm64 (under QEMU)
osb build -machine x86_64      base-image            # bare-metal x86_64 (UEFI); write with osb flash
osb build -distro  debian      base-image            # Debian instead of Alpine
osb build -distro  ubuntu      base-image
```

Verified boot in QEMU:

```sh
osb build -machine qemu-x86_64-uefi-secureboot base-image
osb run   -machine qemu-x86_64-uefi-secureboot base-image
# boots a signed UKI with the key enrolled; Secure Boot is enforced.
```

On a Secure Boot machine the **build** signs a Unified Kernel Image
(kernel+initramfs+cmdline in one PE, no GRUB, no shim) into the image's ESP, so
the shipped `disk.img` boots signed on real hardware — `osb run` and `osb flash`
just carry it. `osb run` additionally enrolls the certificate as PK/KEK/db so
QEMU enforces it. By default an embedded, public **test** key is used; sign with
your own key instead:

```sh
osb key secure-boot          # writes keys/secureboot/db.{key,crt}
osb build -machine qemu-x86_64-uefi-secureboot base-image   # signs the UKI with it
```

Flashing a Secure Boot image still signed with the public test key prints a
warning — that key is public in git and not secure on real hardware.

Every build also emits a CycloneDX SBOM (`<image>.sbom.json`) of the packages the
image contains. Builds are reproducible: set `SOURCE_DATE_EPOCH` (or accept the
fixed default) and identical inputs produce byte-identical artifacts.

The `*-secureboot-verity` machines extend verified boot into userspace with a
**dm-verity read-only root**: the build hashes the rootfs into a Merkle tree
and folds its root hash into the signed cmdline as a `dm-mod.create` table, so
the kernel mounts the verified `/dev/dm-0` directly (no GRUB, no initramfs) and
a single tampered block fails the boot instead of booting compromised. The
`rootoverlay` unit lays tmpfs overlays over `/etc`, `/var`, and friends so
services that write at boot run unchanged; writes reset on reboot. See
[docs/design/2026-07-02-dm-verity.md](docs/design/2026-07-02-dm-verity.md).

The `qemu-x86_64-uefi-ab` machine builds an A/B dual-slot image with automatic
rollback, using the same GRUB grubenv scheme RAUC and SWUpdate drive — see
[docs/design/ab-updates.md](docs/design/ab-updates.md). The
`qemu-x86_64-uefi-secureboot-ab` machine combines A/B with Secure Boot: one
signed UKI per slot, selected by UEFI boot entries (RAUC's `efi` backend) —
see [docs/design/2026-07-02-secureboot-ab.md](docs/design/2026-07-02-secureboot-ab.md).

### Choosing a bootloader

By default the disk task infers the bootloader from the partition layout: an
`esp` partition means GPT + GRUB EFI, anything else means MBR + syslinux. A
machine can name one explicitly instead:

```python
machine(
    name = "my-board",
    arch = "x86_64",
    bootloader = bootloader(type = "limine"),
    packages = ["limine"],
    ...
)
```

[**limine**](https://codeberg.org/Limine/Limine) covers both x86 firmware modes
from a single `limine.conf`, so BIOS and UEFI variants of a board differ only in
their partition layout — no second bootloader recipe, no `grub-mkimage` run, and
no GRUB module directory in the rootfs. It also reads modern ext4, so a limine
BIOS image keeps extents and metadata checksums instead of the downgraded
filesystem syslinux 6.03 requires. The `limine` unit must be in the image's
package list; it supplies `BOOTX64.EFI`, `limine-bios.sys`, and the deployment
tool the disk task runs.

Two limitations are deliberate:

- **No dm-verity.** A verity machine's root hash is only known after the hash
  tree is computed, which happens *after* the bootloader config has been hashed
  and enrolled into the signed binary. `bootloader(type = "limine")` with
  `verity = True` is rejected rather than silently producing an image whose
  command line lacks the verity table.
- **A/B is selection, not rollback.** See below.

Secure Boot **is** supported — see the next section.

### Secure Boot with limine

`qemu-x86_64-uefi-limine-secureboot` boots a verified chain without a UKI:

```
firmware  --verifies signature-->  limine BOOTX64.EFI
          --verifies enrolled blake2b-->  limine.conf
          --verifies #blake2b on each path-->  kernel + initramfs
```

At signing time osb renders a `limine.conf` whose `path:` and `module_path:`
each carry a `#<blake2b>` suffix, hashes that config, enrols the hash **into**
the EFI binary, and only then signs it. Order is not interchangeable: enrolling
after signing invalidates the signature, and signing before enrolling produces a
binary that enforces nothing.

That last point is the whole reason both halves are done together. Upstream is
explicit that a signed-but-unenrolled limine *"treats Secure Boot as inactive"*
and gives *"no integrity guarantees beyond those of the firmware itself"* — a
verified bootloader that then loads any kernel the config names. osb never
produces that state.

Enrollment is implemented in pure Go (`internal/device/limine.go`) rather than
by shelling out to `limine enroll-config`, because the host tool ships built
for the *target* arch and libc and will not run on the build host. The output is
verified byte-identical to upstream's tool. Signing needs `sbsign`
(`sbsigntools`) on the build host.

Compared with the signed-UKI machines this keeps a real bootloader — a menu,
serial output, multiple entries — at the cost of one more link in the chain.
Prefer `qemu-x86_64-uefi-secureboot` when the shortest verified path matters.

## Targets

**Distros** (`-distro`, or `defaults.distro` in `PROJECT.star`): `alpine`
(default), `debian`, `ubuntu`.

**Machines** (`-machine`, or `defaults.machine`):

| Machine | Arch | Notes |
|---------|------|-------|
| `qemu-x86_64` | x86_64 | BIOS/MBR, the default |
| `qemu-arm64` | arm64 | direct kernel boot under QEMU |
| `qemu-x86_64-uefi` | x86_64 | UEFI + GPT + GRUB EFI |
| `qemu-x86_64-uefi-secureboot` | x86_64 | UEFI Secure Boot (signed UKI) |
| `qemu-arm64-uefi-secureboot` | arm64 | UEFI Secure Boot (signed UKI, AAVMF) |
| `qemu-x86_64-uefi-secureboot-verity` | x86_64 | Secure Boot + dm-verity verified read-only root |
| `qemu-arm64-uefi-secureboot-verity` | arm64 | Secure Boot + dm-verity verified read-only root |
| `qemu-x86_64-uefi-ab` | x86_64 | A/B dual-slot rootfs with rollback |
| `qemu-x86_64-uefi-secureboot-ab` | x86_64 | Secure Boot + A/B (one signed UKI per slot) |
| `qemu-x86_64-limine` | x86_64 | BIOS/MBR + limine |
| `qemu-x86_64-uefi-limine` | x86_64 | UEFI + GPT + limine |
| `qemu-x86_64-uefi-limine-ab` | x86_64 | limine + A/B dual-slot (selection only, no auto-rollback) |
| `qemu-x86_64-uefi-limine-secureboot` | x86_64 | Secure Boot via signed limine + enrolled config hash |
| `x86_64` | x86_64 | bare-metal PC (UEFI); build then `osb flash` |

**Images** (bundled): `base-image` (minimal boot), `ssh-image`, `dev-image`,
`installer-image` (bootable installer, see below), plus Alpine app demos
(`nodejs-image`, `python-image`, `docker-image`, …).

## Installing onto a machine

`osb flash` writes a prebuilt image onto a device you name. The
`installer-image` is the other half — a live USB that boots on the target and
asks what to do with it:

```sh
osb build -machine x86_64 installer-image
osb flash installer-image /dev/sdX     # write the stick
# boot the target from it, then:
osb-installer                          # guided
osb-installer -config install.conf     # unattended, for fleets
osb-installer -dry-run                 # print the plan, change nothing
```

It offers a target disk, optional **LUKS2 full-disk encryption**, **Secure Boot**
(installs the signed UKI), hostname, and accounts. Encryption and Secure Boot
require UEFI — a BIOS layout has no ESP to hold the unencrypted kernel and
bootloader, so `Validate` rejects that combination rather than producing a disk
that never boots.

The install sequence is generated as data and unit-tested command-by-command
(`go test ./internal/installer`), because an installer cannot be exercised in
CI without a disk to destroy. The layout is fixed (ESP + root); there is no
partition editor or install-alongside yet. See
[docs/design/installer.md](docs/design/installer.md).

## Customizing a project

A project is a `PROJECT.star` plus optional `units/`, `images/`, `machines/`,
and `classes/` directories. A fresh project file is just name + version +
defaults — the bundled standard library provides everything else. The stdlib
is injected at the lowest priority, so anything you define in the project
**overrides** the bundled default of the same name. To change a package's
build, drop a unit with that name under `units/`; to add a board, drop a
machine under `machines/`. Image definitions go under `images/` — they are
evaluated after every module's units, so their closures resolve against the
full stdlib.

Packages from the distro feeds (`alpine.main`, `debian.main`, `ubuntu.main`,
…) can be named directly in `deps` or image artifact lists; their units
materialize lazily from the checked-in indexes. When a name exists both as a
source-built unit and in a feed, the source unit wins by default. Per-unit
routing is controlled by `prefer_modules` pins, keyed by distro:

```python
prefer_modules = {"alpine": {"xz": "alpine.main"}},   # in project() — optional
```

The stdlib distro modules already declare the universal pins as defaults in
their `MODULE.star` (`module-alpine` pins xz/zstd/util-linux/curl/kmod to
`alpine.main` because module-core's monolithic source builds collide with the
feeds' split library packaging — the rationale lives next to each pin), so a
project normally needs no `prefer_modules` at all. A project-level entry
overrides a default per unit, and pinning a name to `""` restores default
module-priority resolution (i.e. the source-built unit).

The most common customization — "the stock base image plus my packages" —
composes from the baseline package sets in `classes/baseline.star` instead of
copying `base-image`'s lists, so the image keeps tracking stdlib fixes to the
base set:

```python
# images/my-image.star
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_ARTIFACTS", "BASE_DISTRO_ARTIFACTS")

image(
    name = "my-image",
    artifacts = BASE_ARTIFACTS + ["efitools", "htop"],
    distro_artifacts = BASE_DISTRO_ARTIFACTS,
)
```

Point `defaults.image` at it (or pass the name to `osb build`/`osb run`).
`ALPINE_BASE` and `APT_BASE` are also exported individually for per-distro
composition. When resolution is in doubt — same name in several modules, or a
`prefer_modules` pin in play — `osb desc <unit>` prints which module each
distro's images actually resolve the name from.

To develop applications against an image without writing units, generate an
SDK from a built image:

```sh
osb build my-image
osb sdk  my-image           # bakes osb/sdk-<project>-my-image:<distro>-<arch>
osb sdk  my-image -shell    # interactive shell, $PWD mounted at /work
```

The SDK is a docker image pairing the ABI-matched toolchain (musl or glibc,
same dispatch as builds) with the union sysroot of the image's closure —
every header, library, and pkg-config file the image's packages staged. The
environment is preset (`CC`, `CFLAGS`, `LDFLAGS`, `PKG_CONFIG_PATH` point at
`/opt/osb/sysroot`), so inside it `$CC $CFLAGS $LDFLAGS app.c -o app` (or a
`./configure`/`cmake` invocation) links against exactly the library versions
the target runs. Cross-arch SDKs run under binfmt like builds do. An
`environment-setup` script lands next to the sysroot in
`build/<distro>/<image>.<machine>/sdk/` for non-docker consumers.

A custom image with its own users (any number; each non-root user owns their
home directory):

```python
# images/my-image.star
load("@core//classes/image.star", "image")
load("@core//classes/users.star", "user")
load("@core//units/base/base-files.star", "base_files")

base_files(name = "base-files-mine", users = [
    user(name = "root",  uid = 0,    gid = 0,    home = "/root"),
    user(name = "user",  uid = 1000, gid = 1000, password = "password"),
    user(name = "alice", uid = 1001, gid = 1001, password = "secret"),
])

image(
    name = "my-image",
    artifacts = ["linux", "bash"],
    distro_artifacts = {"alpine": [
        "base-files-mine", "busybox", "busybox-binsh", "musl",
        "kmod", "util-linux", "e2fsprogs", "eudev",
        "openrc", "apk-tools", "network-config", "dhcpcd", "openssh",
    ]},
)
```

Units that must ship non-root-owned paths declare them with
`owners = {"/path": "uid:gid"}` — the ownership is stamped into the package
itself, so image-time and on-target installs agree.

## Commands

```
init <project-dir>    Create a new project
build [units...]      Build units (--machine, --distro, --force, --clean, --dry-run)
run                   Run an image in QEMU (--machine, --display, --boot-test)
flash <unit> <dev>    Write an image to a disk/SD card (flash list to enumerate)
sdk <image>           Generate an app-dev SDK for a built image (-shell to enter it)
container             Manage the build container (build, shell, status)
repo                  Manage the local package repository
config                View and edit project configuration
desc <unit>           Describe a unit or target
refs <unit>           Show reverse dependencies
graph                 Visualize the dependency DAG
log [unit]            Show a build log
update-feeds          Refresh a module's feed indexes (run inside a module repo)
key ...               Manage signing keys: generate|info (apk repo), secure-boot (UKI/PK/KEK/db)
clean                 Remove build artifacts
version               Print the version
```

## Documentation

- [docs/naming-and-resolution.md](docs/naming-and-resolution.md) — how a
  package name resolves to one unit: module priority, distro visibility,
  `prefer_modules` pins, feeds as synthetic modules, `provides`, `replaces`.
- [docs/build-environment.md](docs/build-environment.md) — the merged
  dependency sysroot and the shared env (executor, `container shell`, `sdk`).
- [docs/testing.md](docs/testing.md) — the test layers and the full-matrix
  suites in `test-suites.yaml` (`make test-full`).
- [docs/on-device-upstream-feeds.md](docs/on-device-upstream-feeds.md) — the
  dormant `upstream-feeds` opt-in for installing upstream distro packages on
  a dev device.
- [docs/design/](docs/design/) — design notes (Secure Boot signing, dm-verity,
  A/B updates, roadmap).

```sh
make docs         # render godoc comments to Markdown under docs/api/
make docs-serve   # browse the API docs at http://localhost:6060
```

Design notes live under `docs/design/`.
