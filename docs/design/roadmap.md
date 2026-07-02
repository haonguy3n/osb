# osb roadmap

Prioritized development tasks (value-to-effort), with current status. Generated
from a codebase analysis and kept as the working backlog.

## Done

- **Verified boot via signed UKI** — kernel+initrd+cmdline assembled into one
  signed EFI binary, booted under enforced Secure Boot in QEMU to a login
  prompt (no GRUB, no shim). `internal/device/secureboot.go`.
- **Project-owned Secure Boot keys** — `osb key secure-boot` generates
  `keys/secureboot/db.{key,crt}`; signing prefers the project key over the
  embedded public test key.
- **Build-time image signing** — a Secure-Boot machine signs a UKI into the
  canonical `disk.img` ESP during the build, so the shipped image boots signed
  on real hardware; `osb run` only enrolls the cert and boots it. Machine-level
  `secure_boot` field added.
- **CycloneDX SBOM per image** — real SHA-1 hashes, deterministic serial.
  `internal/sbom`.
- **Reproducible artifacts** — SOURCE_DATE_EPOCH honored; apk/deb tar mtimes and
  build dates clamped; two builds of identical inputs are byte-identical.
- **CI** — vet/gofmt/test/build + bundled-machine check; gated self-hosted
  boot-test matrix. `.github/workflows/ci.yml`.
- **UEFI boot fixes** — GPT backup reserve (partition geometry) and explicit
  Alpine initramfs regeneration (bind `/dev` for mkinitfs on nodev mounts).
- **Bundled bare-metal `x86_64` machine**; distro/machine test coverage.
- **arm64 UEFI Secure Boot** — a signed arm64 UKI (BOOTAA64.EFI + arm64 stub)
  booted under enforced Secure Boot on AAVMF to an SSH login and health check.
  Firmware, EFI stub, boot path, and OVMF/AAVMF selection are arch-parameterized;
  Secure-Boot machines install `mkinitfs` directly (they drop GRUB, which used to
  pull it transitively) so the initramfs the UKI embeds can resolve the labeled
  root. `qemu-arm64-uefi-secureboot` machine added.

- **dm-verity verified root** — the rootfs is hashed into a Merkle tree whose
  root hash is folded into the Secure-Boot-signed kernel command line as a
  `dm-mod.create` table; the kernel builds the verified device and mounts
  `root=/dev/dm-0` directly, with no GRUB and no initramfs. Validated on x86: a
  clean image boots to a login prompt on the verified read-only root, and a
  single tampered data block makes the kernel refuse the block and panic rather
  than boot compromised. The hash-tree formatter is pure Go, cross-checked
  byte-for-byte against `veritysetup`. `qemu-{x86_64,arm64}-uefi-secureboot-verity`
  machines added. `docs/design/2026-07-02-dm-verity.md`.

- **Read-only-root ergonomics for verity** — a `rootoverlay` unit lays
  tmpfs-backed overlayfs over `/etc`, `/var`, `/root`, and `/home` in the OpenRC
  sysinit runlevel, so services that write at boot run unchanged on the immutable
  verified root; writes live in RAM and reset on reboot. Both verity machines pull
  it via `distro_packages`. Validated on x86: `ssh-image` boots on the verity root
  to an SSH login, with host keys generated onto the `/etc` overlay upper layer.
  `stdlib/module-core/units/base/rootoverlay.star`.

## Open

1. **Secure Boot + A/B** — sign a UKI per slot so the A/B machine also enforces
   Secure Boot; a verity hash partition per slot. (M)
2. **Measured boot / TPM PCR policy** — opt-in; gate secrets on PCRs. (L)
3. **Image size optimization** — strip, drop docs/man/locale, optional read-only
   squashfs (pairs with dm-verity). (M)
4. **Fix duplicate-provides collision** upstream and flip
   `globalAllowDuplicateProvides` back to strict. (M)
5. **Finish removing internal/module** — the git fetch helpers remain, used only
   by the e2e test and check_debug; delete once those are rewired. (S)

Items 1–3 cluster on one epic; do them in order. Items needing boot validation or
deep loader surgery are best done attended, not in an unattended batch.
