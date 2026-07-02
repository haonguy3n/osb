# Secure Boot + A/B: a signed UKI per slot

## Why

The A/B machine boots through GRUB, whose grubenv scheme RAUC and SWUpdate
drive. Secure Boot machines drop GRUB entirely — the signed Unified Kernel
Image is the whole boot path, and the cmdline (root=, rauc.slot=) is inside
the signature. Those two designs meet in the middle: with two slots the root
device differs per slot, so one signed UKI cannot boot both, and an unsigned
grub.cfg choosing the slot would reopen the hole Secure Boot closed.

The resolution is **one signed UKI per slot**. Each slot's UKI carries its own
`root=LABEL=rootfs-<slot> rw rauc.slot=<slot>` inside the signed cmdline, and
the slot choice moves out of bootloader config files into **UEFI boot
entries** — BootOrder and BootNext, the firmware's own signed-firmware-managed
state. That is exactly the model RAUC's `efi` bootloader backend drives with
efibootmgr, so update-framework integration is preserved without a bootloader.

## Layout

```
esp        FAT   /EFI/osb/a.efi          signed UKI, root=LABEL=rootfs-a rauc.slot=a
                 /EFI/osb/b.efi          signed UKI, root=LABEL=rootfs-b rauc.slot=b
                 /EFI/BOOT/BOOTX64.EFI   copy of slot A's UKI (fallback)
rootfs-a   ext4  the OS (installed at build time; the initial slot)
rootfs-b   ext4  empty spare (an update populates it)
```

The build signs a UKI for every slot at image-signing time — slot B's UKI is
identical to A's except for its cmdline, ready for the day an update populates
the partition. The removable-media fallback path carries slot A's UKI so a
board with blank NVRAM (first boot, replaced mainboard) boots the initial slot
with no setup; once the OS creates the two boot entries (efibootmgr, or RAUC's
efi backend on first update), BootOrder takes over.

## Boot selection and rollback (RAUC efi backend)

- `osb run` enrolls the two boot entries into the OVMF variable store
  alongside the Secure Boot certificate (`virt-fw-vars
  --append-boot-filepath /EFI/osb/a.efi ...`), so QEMU boots the same way
  real hardware does after entry creation.
- An update writes the inactive slot and sets **BootNext** to its entry: the
  firmware boots it exactly once. If the new slot comes up healthy, the
  updater marks it good by putting it first in **BootOrder**; if it never
  confirms, the next boot follows the unchanged BootOrder — automatic
  rollback with no bootloader logic.
- On the device this is `rauc` with `bootloader=efi` in system.conf (or plain
  efibootmgr); the kernel's `rauc.slot=` from the signed cmdline tells RAUC
  which slot booted.

## What osb does

- `machine()` with `secure_boot` and two ext4 partitions triggers the A/B
  signing path (the same two-ext4 rule that triggers the GRUB A/B layout).
- The image class creates `/EFI/osb` on the ESP and skips GRUB.
- At build-time signing, osb signs one UKI per slot with the per-slot cmdline
  appended to the machine's base cmdline, installs each at
  `/EFI/osb/<slot>.efi`, and copies the initial slot's UKI to the
  removable-media fallback path.
- `osb run` appends one permanent UEFI boot entry per slot when enrolling the
  Secure Boot certificate into the per-run OVMF vars.

The bundled `qemu-x86_64-uefi-secureboot-ab` machine wires this together.

## Scope / follow-ons

- **dm-verity per slot** (a `verity-hash` partition per slot, each slot's root
  hash in its own signed cmdline) is designed to compose with this — the slot
  B hash can only be computed when an update populates the slot, so it lands
  with the update-bundle tooling. Building a verity A/B machine today fails
  with a clear error.
- The apt-family UEFI disk path predates A/B and Secure Boot and supports
  neither; the machine is Alpine-first like the other Secure Boot machines.
- On-device entry creation on real hardware needs efivarfs + efibootmgr (or
  RAUC); until entries exist the fallback path boots slot A.
