# Secure Boot test keys

`db.key` / `db.crt` are a **test-only** self-signed keypair, embedded into the
`osb` binary and used by `osb run` to validate the UEFI Secure Boot path under
QEMU. On a Secure Boot machine, `osb run` assembles the built kernel, initramfs,
and command line into a signed Unified Kernel Image, installs it at the ESP's
`EFI/BOOT/BOOTX64.EFI` on a throwaway copy of the disk, and enrolls this
certificate as PK/KEK/db in a per-run OVMF variable store so the firmware
verifies and boots the UKI directly.

They are committed on purpose (deterministic, no keygen at run time) and are
**not secret** — the private key is public in git. Never use them to sign
anything shipped to real hardware. Production signing keys are a separate,
project-supplied mechanism (a project-owned Secure Boot key).
