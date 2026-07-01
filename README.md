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
- **Secure Boot only** (`--machine qemu-x86_64-uefi-secureboot`): `ovmf`,
  `systemd-ukify`, `mtools`, and `python3-virt-firmware`. `osb run` names any
  missing package before launching.

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

`osb run` assembles the kernel, initramfs, and command line into a signed
Unified Kernel Image (no GRUB, no shim) and enrolls the certificate as PK/KEK/db.
By default it uses an embedded, public **test** key. To sign with your own key:

```sh
osb key secure-boot          # writes keys/secureboot/db.{key,crt}
osb run -machine qemu-x86_64-uefi-secureboot base-image   # now signs with it
```

Every build also emits a CycloneDX SBOM (`<image>.sbom.json`) of the packages the
image contains. Builds are reproducible: set `SOURCE_DATE_EPOCH` (or accept the
fixed default) and identical inputs produce byte-identical artifacts.

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
| `x86_64` | x86_64 | bare-metal PC (UEFI); build then `osb flash` |

**Images** (bundled): `base-image` (minimal boot), `ssh-image`, `dev-image`,
plus Alpine app demos (`nodejs-image`, `python-image`, `docker-image`, …).

## Customizing a project

A project is a `PROJECT.star` plus optional `units/`, `machines/`, and
`classes/` directories. The bundled standard library is injected at the lowest
priority, so anything you define in the project **overrides** the bundled
default of the same name. To change a package's build, drop a unit with that
name under `units/`; to add a board, drop a machine under `machines/`.

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
module                Manage external (local override) modules
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
