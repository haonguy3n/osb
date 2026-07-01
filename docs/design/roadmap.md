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

## Open

1. **dm-verity rootfs** — hash-tree the rootfs, embed the root hash in the signed
   UKI cmdline; extends the trust chain past the bootloader. (L, high risk)
2. **Secure Boot + A/B** — sign a UKI per slot so the A/B machine also enforces
   Secure Boot. (M)
3. **Measured boot / TPM PCR policy** — opt-in; gate secrets on PCRs. (L)
4. **Image size optimization** — strip, drop docs/man/locale, optional read-only
   squashfs (pairs with dm-verity). (M)
5. **Fix duplicate-provides collision** upstream and flip
   `globalAllowDuplicateProvides` back to strict. (M)
6. **Finish removing internal/module** — the git fetch helpers remain, used only
   by the e2e test and check_debug; delete once those are rewired. (S)

Items 1–5 cluster on one epic; do them in order. Items needing boot validation or
deep loader surgery are best done attended, not in an unattended batch.
