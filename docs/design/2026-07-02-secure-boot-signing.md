# Design: first-class image signing + Secure Boot

Status: proposed (for review)
Date: 2026-07-02

## Goal

Make verified boot a headline osb capability: an osb image can be signed with a
**project-owned key** and boot on UEFI hardware (and QEMU) with Secure Boot
enforced, end to end — firmware → bootloader → kernel → (optionally) rootfs — so
a device only runs software the project signed.

This supersedes the inherited run-time-only Secure Boot path (which signs GRUB
on a throwaway QEMU disk copy at `osb run` and cannot boot the kernel under SB
because GRUB demands a Microsoft shim). See the yoe UEFI plan for that history.

## The core decision: UKI as the primary trust chain

The self-signed, shimless path that actually boots to userspace under enforced
Secure Boot is a **Unified Kernel Image (UKI)**: kernel + initramfs + kernel
command line + os-release, linked into a single EFI (PE) executable on top of an
EFI stub, then signed as one artifact.

```
EFI/BOOT/BOOTX64.EFI    ← signed UKI on x86_64 (stub + .linux + .initrd + .cmdline + .osrel)
EFI/BOOT/BOOTAA64.EFI   ← signed UKI on arm64  (same layout, arm64 stub)
```

The removable-media boot name, the systemd EFI stub, the ukify `--efi-arch`
token, and the QEMU firmware (OVMF on x86_64, AAVMF on arm64) are all selected
from the machine's arch, so the same signing path produces a bootable UKI for
either architecture. The firmware verifies that one binary against the enrolled
`db` and executes it; its stub sets up the kernel and hands over the embedded
initramfs and cmdline.
**No GRUB, no shim, no `shim_lock` gate** — which is exactly the wall the current
path hits. This is the modern appliance/embedded pattern (systemd-boot/`ukify`)
and it composes cleanly with A/B updates later (two UKIs, or two firmware boot
entries).

Trade-off: a UKI is one fixed kernel+cmdline+initrd per image — no interactive
boot menu. For osb's appliance/fleet target that is a feature, not a loss;
variation is handled at the image/partition level, not a boot menu.

GRUB stays the default for **non-Secure-Boot** images (unchanged). Secure Boot
selects the UKI path.

### Alternative trust models (kept, not primary)

- **Microsoft/shim chain** (Debian/Ubuntu): ship the distro's MS-signed shim +
  signed GRUB, enroll Microsoft keys. Boots on stock-Secure-Boot PCs with no key
  enrollment. Offered as an opt-in for users who need to boot unmodified-firmware
  machines; not available for Alpine (no MS-signed shim). This is a later,
  additive path.
- **Self-signed shim**: build shim from source with the project cert baked in.
  More moving parts than a UKI for the same result; deferred unless a project
  needs GRUB's menu under SB.

## Key management

Secure Boot signing keys are **project-owned**, sourced with the same cascade
osb already uses for apk signing keys, extended with a dev fallback:

1. Project-declared key: `secure_boot(key = "keys/db.key", cert = "keys/db.crt")`
   in the machine or project — PEM key + X.509 cert the project controls.
2. `osb key secure-boot generate` — create a project SB keypair under `keys/`
   (self-signed, long-lived), analogous to `osb key` for apk.
3. Embedded **test** key fallback (the current `internal/device/secureboot`
   keypair), used only when no project key is set, and only for QEMU. Building a
   real-hardware image with the test key is a hard error — the test key is public
   in git and must never sign a shipped artifact.

One self-signed cert serves as PK/KEK/db for a self-managed platform (the common
appliance case). Splitting PK/KEK/db is possible later but not required.

## Where signing happens: build time (into the artifact)

Signing moves from `osb run` (QEMU-only) to **image build**, so the signed UKI
lives in the image and boots real hardware with no osb host in the loop:

1. Assemble rootfs (unchanged).
2. Build the UKI: locate kernel + initramfs in the rootfs, embed them with the
   cmdline and os-release onto the EFI stub (`ukify`, or `objcopy` onto a stub
   package's `linux*.efi.stub`).
3. Sign the UKI with the project key (`sbsign`).
4. Place the signed UKI on the ESP at `EFI/BOOT/BOOTX64.EFI`.

The private key must not enter the hermetic build container. Two options to
decide (see Open questions): sign as a **host post-build step** on the assembled
ESP (key stays on host, matches how real signing/HSM works), or pull
`sbsigntool` into the build container and sign in-band (hermetic, but the key
enters the sandbox). Recommendation: **host post-build signing** — keeps the key
out of build layers and is the honest model for production keys.

## Enrollment

- **QEMU**: reuse the existing OVMF split-firmware + `virt-fw-vars` enrollment
  (already implemented) — enroll the project cert as PK/KEK/db into a per-run
  vars store, boot with `smm=on` + secure pflash. Works today for the bootloader
  stage; with a UKI it now reaches userspace.
- **Real hardware**: osb emits the cert (DER + `.auth`) as a build artifact and
  documents enrollment via firmware setup, plus an optional auto-enroll payload
  (place `PK.auth`/`KEK.auth`/`db.auth` on the ESP for firmware that supports
  "enroll from disk", or a one-shot enroller EFI app). Enrollment on physical
  boards is inherently a per-device provisioning step; osb provides the keys and
  the artifacts, not magic.

## Project/machine API

Opt in declaratively; no new global default:

```starlark
# machine (or image) turns on Secure Boot + names the signing key
secure_boot(
    key  = "keys/db.key",   # project-owned; omit -> embedded test key (QEMU only)
    cert = "keys/db.crt",
    # trust = "self-signed" (default UKI) | "shim" (MS chain, apt distros)
)
```

An image whose machine has `secure_boot(...)` builds the UKI path and produces a
signed image + enrollment artifacts. Without it, nothing changes.

## Implementation phases

1. **Docs + API** (this doc → target-state reference) and the `secure_boot(...)`
   builtin + project key cascade.
2. **UKI builder** in the image class: assemble + sign (host post-build),
   place on ESP. Prove it boots to a login prompt under enforced SB in QEMU
   (closes the `shim_lock` gap).
3. **Build-time signing + enrollment artifacts** for real hardware; `osb key
   secure-boot generate`; test-key-on-hardware guard.
4. **Verify**: QEMU boot test asserts SB is enabled in the guest
   (`bootctl status` / EFI var) and an unsigned image is rejected.
5. (Later, additive) shim/Microsoft trust model for apt distros.

## Open questions (for review)

1. **Sign on host vs in-container?** Recommendation: host post-build (key never
   enters the sandbox). Confirm.
2. **EFI stub source**: systemd `ukify`/stub (clean, but pulls systemd on Alpine)
   vs a minimal stub + `objcopy`. Recommendation: `ukify` where the distro ships
   it; `objcopy` + the kernel's own EFI stub as the Alpine-friendly fallback.
3. **initramfs on Alpine**: the labeled root (`root=LABEL=rootfs`) needs an
   initramfs to find the device, and the UKI embeds it. Secure-Boot machines
   ship no GRUB (the UKI replaces it), and GRUB is what used to pull `mkinitfs`
   into the Alpine closure — so the Secure-Boot machines install `mkinitfs`
   explicitly to keep the initramfs in the image.
4. **Where the UKI lands**: `EFI/BOOT/BOOT<arch>.EFI` (removable-media default,
   no firmware boot entry needed) vs `EFI/Linux/*.efi` (needs a boot entry). Rec:
   the removable-media path for zero-config boot.
```
