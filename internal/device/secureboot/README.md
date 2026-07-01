# Secure Boot test keys

`db.key` / `db.crt` are a **test-only** self-signed keypair, embedded into the
`osb` binary and used by `osb run` to validate the UEFI Secure Boot path under
QEMU: the GRUB EFI binary in a built image's ESP is re-signed with this key on a
throwaway copy of the disk, and the same certificate is enrolled as PK/KEK/db in
a per-run OVMF variable store so the firmware actually enforces the signature.

They are committed on purpose (deterministic, no keygen at run time) and are
**not secret** — the private key is public in git. Never use them to sign
anything shipped to real hardware. Production signing keys are a separate,
project-supplied mechanism (not yet implemented).
