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
| `x86_64` | x86_64 | bare-metal PC (UEFI); build then `osb flash` |

**Images** (bundled): `base-image` (minimal boot), `ssh-image`, `dev-image`,
plus Alpine app demos (`nodejs-image`, `python-image`, `docker-image`, …).

## Customizing a project

A project is a `PROJECT.star` plus optional `units/`, `images/`, `machines/`,
and `classes/` directories. The bundled standard library is injected at the
lowest priority, so anything you define in the project **overrides** the
bundled default of the same name. To change a package's build, drop a unit
with that name under `units/`; to add a board, drop a machine under
`machines/`. Image definitions go under `images/` — they are evaluated after
every module's units, so their closures resolve against the full stdlib.

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

```sh
make docs         # render godoc comments to Markdown under docs/api/
make docs-serve   # browse the API docs at http://localhost:6060
```

Design notes live under `docs/design/`.
