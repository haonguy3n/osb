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
- **CycloneDX SBOM per image** — real SHA-1 hashes, deterministic serial.
  `internal/sbom`.
- **Reproducible artifacts** — SOURCE_DATE_EPOCH honored; apk/deb tar mtimes and
  build dates clamped; two builds of identical inputs are byte-identical.
- **CI** — vet/gofmt/test/build + bundled-machine check; gated self-hosted
  boot-test matrix. `.github/workflows/ci.yml`.
- **UEFI boot fixes** — GPT backup reserve (partition geometry) and explicit
  Alpine initramfs regeneration (bind `/dev` for mkinitfs on nodev mounts).
- **Bundled bare-metal `x86_64` machine**; distro/machine test coverage.

## Open (verified-boot epic — sequence to avoid reworking secureboot.go)

1. **Build-time image signing** — move UKI assembly/signing from `osb run` into
   the image build so real hardware boots a signed image with no osb host in the
   loop. Host post-build signing; keep the key out of the build container. (L)
2. **Guard the embedded test key** — hard error when a real-hardware image is
   built/flashed with the public test key. Cheap, pure security. (S)
3. **dm-verity rootfs** — hash-tree the rootfs, embed the root hash in the
   signed UKI cmdline; extends the trust chain past the bootloader. (L, high risk)
4. **arm64 UEFI Secure Boot** — parameterize `installUKIToESP`/firmware by arch
   (BOOTAA64.EFI + AAVMF + arm64 stub). (M)
5. **A/B (dual-slot) updates** — two-rootfs layout + boot-slot selection +
   atomic swap/rollback; composes with UKIs. (L, high risk)
6. **Measured boot / TPM PCR policy** — opt-in; gate secrets on PCRs. (L)

## Open (quality / breadth)

7. **Image size optimization** — strip, drop docs/man/locale, optional
   read-only squashfs (pairs with dm-verity). (M)
8. **Remove the external git-module system** — osb bundles its stdlib; keep only
   local module overrides, drop git clone/sync + the `module` command. (M, risky
   loader surgery — do attended)
9. **Fix duplicate-provides collision** upstream and flip
   `globalAllowDuplicateProvides` back to strict. (M)
10. **CLI papercuts** — `--all` parsed then discarded; `module info`/
    `check-updates` stubs; custom-command stdout wired to nil. (S)

Items 1–6 cluster on one epic; do them in order. Items needing boot validation or
deep loader surgery are best done attended, not in an unattended batch.
