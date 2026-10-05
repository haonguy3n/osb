# osb

`osb` builds bootable Alpine, Debian and Ubuntu images for x86_64 and arm64
from one Go binary. Every image is a disk image you can `dd` or `bmaptool` onto
an SD card, SSD or USB stick, and optionally a hybrid ISO that installs it.

- **Distro packages first.** Name any Alpine/Debian/Ubuntu package and it is
  pulled from the distro feed. Your own software is a small unit next to it.
- **Machines are hardware, images are policy.** A machine says what the board
  is (arch, firmware, console, kernel). An image says what runs on it:
  bootloader, partition layout and security features.
- **Security features are flags:** `secureboot`, `verity`, `readonly`,
  `encrypt`, `tpm`, `ab`.
- **wic-like layouts** with `part()`, or a sensible default derived from the
  features.

## Requirements

- Go 1.25+ and Docker.
- For `osb run`: QEMU (`qemu-system-x86`, `qemu-system-arm`), `ovmf`,
  `qemu-efi-aarch64`.
- For `secureboot`/`uki` images: `systemd-ukify`, `sbsigntool`, `mtools`, and
  `python3-virt-firmware` to enroll keys in QEMU.
- For `tpm` images in QEMU: `swtpm`.

```sh
make build            # ./osb
```

## Quick start

```sh
osb init -distro ubuntu myproject
cd myproject
osb build                       # builds my-image for qemu-x86_64
osb run                         # boots it (serial console on stdio)
osb run -boot-test              # boot, ssh in as root, power off
osb run -test tests/smoke.sh    # boot, run the script in the guest, power off
osb flash my-image /dev/sdX     # write it to a disk (uses bmaptool when present)
```

`osb init` creates:

```
PROJECT.star                project name, default machine, image and distro
images/my-image.star        the base system plus your app
units/hello-cpp.star        a C++ app (CMake) built from units/hello-cpp/
units/libgreet.star         a C++ shared library it links against
tests/smoke.sh              checks run in the booted image by `osb run -test`
```

## Machines

| Machine            | Arch   | Firmware | Use                          |
|--------------------|--------|----------|------------------------------|
| `qemu-x86_64`      | x86_64 | UEFI     | default, QEMU q35            |
| `qemu-x86_64-bios` | x86_64 | BIOS     | QEMU with legacy BIOS        |
| `qemu-arm64`       | arm64  | UEFI     | QEMU virt                    |
| `x86_64`           | x86_64 | UEFI     | PCs, `osb flash` to a disk   |
| `arm64`            | arm64  | UEFI     | arm64 boards/servers         |

Pick one with `-machine`, or add your own under `machines/`:

```python
machine(
    name = "my-board",
    arch = "arm64",
    firmware = "uefi",              # or "bios" (x86_64 only)
    console = "ttyS0",
    cmdline = "quiet",
    kernel = {"alpine": "linux-lts", "debian": "linux-image-arm64", "ubuntu": "linux-image-generic"},
    packages = ["linux-firmware"],
    qemu = qemu_config(machine = "virt", memory = "2G", ports = ["2222:22"]),
)
```

## Images

```python
load("@core//classes/image.star", "image")
load("@core//classes/baseline.star", "BASE_DISTRO_PACKAGES", "BASE_PACKAGES", "BASE_SERVICES")
load("@core//classes/users.star", "user")

image(
    name = "my-image",
    packages = BASE_PACKAGES + ["htop", "hello-cpp"],
    distro_packages = BASE_DISTRO_PACKAGES,
    services = BASE_SERVICES,
    bootloader = "limine",          # grub (default on UEFI), limine, uki
    features = ["readonly"],
    users = [user("root", password = "secret"), user("dev", groups = ["wheel"])],
    hostname = "box",
    timezone = "UTC",
    iso = True,                     # also build my-image.iso (installer)
)
```

Bundled images: `base-image` (boot + SSH), `dev-image` (plus tools and a
`user`/`password` account) and `secure-image` (Secure Boot, dm-verity,
TPM-sealed encrypted `/data`).

The default `root` account has no password and SSH allows it. This is for
development: set `users` before you ship an image.

### Bootloaders

| `bootloader` | Firmware   | Notes                                                    |
|--------------|------------|----------------------------------------------------------|
| `grub`       | UEFI       | default on UEFI; A/B with automatic rollback             |
| `limine`     | UEFI, BIOS | default on BIOS; the only one used for the ISO           |
| `uki`        | UEFI       | Unified Kernel Image, signed when `secureboot` is on     |

Kernels and the initramfs live on the ESP (or a FAT boot partition on BIOS),
so every bootloader reads them the same way.

### Features

| Feature      | What it does                                                                 |
|--------------|------------------------------------------------------------------------------|
| `secureboot` | Boots a signed UKI. Uses `keys/secureboot/db.{key,crt}` (`osb key secure-boot`), else a public test key with a warning. |
| `verity`     | dm-verity root. The root hash is in the signed kernel command line. Needs `secureboot`, implies `readonly`. |
| `readonly`   | Root mounted read-only under a tmpfs overlay. Writes vanish on reboot. Use `/data` for state. |
| `encrypt`    | `/data` is LUKS2, formatted on first boot with a random key sealed to the TPM. A recovery key is printed once. Needs `tpm`. |
| `tpm`        | TPM support in the initramfs and `osb-tpm seal/unseal` on the device. `osb run` starts `swtpm`. |
| `ab`         | Two root slots (`root-a`, `root-b`) and a shared `/data`. GRUB falls back to the other slot after a failed boot. |

`osb-tpm` seals any secret to PCRs:

```sh
osb-tpm seal 7 secret.bin /data/secret     # writes pub/priv
osb-tpm unseal 7 /data/secret > secret.bin # only works in the same boot state
```

### Partition layouts

Without `layout`, the image gets an ESP, a root (one per A/B slot), a verity
hash partition per root when `verity` is on, and a growing `/data` when the
features need one. Otherwise the root grows to fill the disk on first boot.

Write your own like a wic file:

```python
load("@core//classes/layout.star", "part")

image(
    name = "board-image",
    packages = ["linux"],
    layout = [
        part("esp", fs = "vfat", size = "128M"),
        part("root", mount = "/", size = "auto"),
        part("logs", mount = "/var/log", size = "512M"),
        part("data", mount = "/data", size = "1G", grow = True),
        part("blob", fs = "raw", source = "/usr/share/firmware/blob.bin"),
    ],
)
```

`part(name, size = "auto", fs = "ext4", mount = None, role = None, slot = "", grow = False, encrypt = False, source = None, type = None, offset = None)`

- `fs`: `ext4`, `vfat`, `swap`, `raw` (copied from `source` in the rootfs) or `verity`.
- `size`: `"64M"`, `"2G"` or `"auto"` (content plus headroom).
- `mount`: the rootfs subtree that goes into the partition, plus its `/etc/fstab` entry.
- `grow`: the last partition grows to fill the disk on first boot.
- `type`: GPT type GUID (or MBR type code). Roles get the Discoverable Partitions Specification GUIDs by default.
- `offset`: start of the partition (`"4M"`, `"8K"`); otherwise partitions follow each other on 1 MiB boundaries.

### Installer ISO

`iso = True` adds `<image>.iso`, a hybrid BIOS/UEFI ISO that also works when
written to a USB stick. It boots the image's own kernel and initramfs, asks for
a target disk and writes the image to it. On first boot the installed system
grows its last partition to fill the disk.

```sh
osb run -iso my-image        # try it in QEMU against a blank disk
```

The ISO's boot entry gets the machine's `cmdline` as well as its console, so the
installer is visible on a screen as well as on a serial port. Putting
`osb.target=` in the image's `cmdline` installs without prompting:

```python
image(name = "my-image", iso = True, cmdline = "osb.target=/dev/sda", ...)
```

Installing into a virtual machine needs the initrd to have the driver for that
machine's disk controller. It carries virtio, NVMe, IDE/SATA, USB, the LSI
Logic and MegaRAID SCSI controllers, VMware's paravirtual SCSI and Hyper-V, so
VirtualBox (IDE/SATA) and VMware (SATA/NVMe, LSI Logic) both work; VMware's
PVSCSI needs a kernel that has `vmw_pvscsi` — the `linux-virt`/`linux-image-virtual`
flavours the `qemu-*` machines use are stripped down, so build for the `x86_64`
or `arm64` machine when you install onto a virtual machine or real hardware.

## Packages and your own software

Name distro packages in `packages` or `distro_packages`. Build your own
software with a class. Local sources work, so the code can live in the project:

```python
load("@core//classes/cmake.star", "cmake")

cmake(
    name = "hello-cpp",
    version = "1.0.0",
    source = "./hello-cpp",                  # relative to this file
    deps = ["libgreet"],                     # build against another unit
    runtime_deps = ["libgreet"],             # and ship it next to the app
    distro_deps = {"ubuntu": ["zlib1g-dev"], "debian": ["zlib1g-dev"], "alpine": ["zlib-dev"]},
    distro_runtime_deps = {"ubuntu": ["zlib1g"], "debian": ["zlib1g"], "alpine": ["zlib"]},
)
```

Build dependencies are staged into a sysroot (`CMAKE_PREFIX_PATH`,
`PKG_CONFIG_PATH`, `CFLAGS` and `LDFLAGS` point at it). Runtime dependencies
become the package's `Depends`/`depend` and are pulled into every image that
installs it. Other classes: `autotools`, `go_binary`, `python_venv`,
`nodejs_app`, `binary` (prebuilt downloads).

## Commands

```
init <dir>             create a project (-distro, -machine)
build [units]          build the default image or the named units (-machine, -distro, -force, -all, -j)
run [image]            boot an image in QEMU (-machine, -distro, -boot-test, -test, -iso, -daemon, -memory, -disk-size)
flash <image> <disk>   write an image to a disk (-machine, -distro, -yes); flash list shows disks
key [secure-boot]      show the package signing key, or create a Secure Boot key
log [unit]             print the latest build log, or one unit's
clean [units]          remove build output (-all also removes the package repo)
shell                  open a shell in the build container
binfmt                 register qemu-user to build arm64 on x86_64
version                print the version
```

`OSB_PROJECT` sets the project directory; `OSB_CACHE` the download cache.

Each build writes `<image>.img`, `<image>.img.bmap`, `<image>.sbom.json`
(CycloneDX) and `<image>.iso` when `iso = True` under
`build/<distro>/<image>.<machine>/destdir/`.

## CI

`.github/workflows/ci.yml` runs `go test`, then for every pull request to
`main` builds the `osb init` project for Ubuntu and Alpine on x86_64 and arm64,
boots each image in QEMU and runs `tests/smoke.sh` in it. x86_64 uses KVM; the
arm64 runners have none, so the arm64 guests run under TCG and take longer.

See [docs/naming-and-resolution.md](docs/naming-and-resolution.md) for how
package names resolve across units, modules and distro feeds.
