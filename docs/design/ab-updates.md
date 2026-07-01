# A/B updates and rollback

osb builds A/B (dual-slot) images that update atomically and roll back
automatically on a failed boot, using the same GRUB `grubenv` scheme that
[RAUC](https://rauc.io) and [SWUpdate](https://swupdate.org) drive — so those
frameworks integrate with no bootloader work.

## Layout

A machine with **two ext4 rootfs partitions** builds an A/B image:

```
esp        FAT   — GRUB EFI + grub.cfg + /EFI/osb/grubenv
rootfs-a   ext4  — the OS (installed at build time; the active slot)
rootfs-b   ext4  — empty spare (an update populates it)
```

See the bundled `qemu-x86_64-uefi-ab` machine. The build installs the OS into the
`root=True` slot and leaves the other empty.

## Boot logic (grubenv ORDER / OK / TRY)

`/EFI/osb/grubenv` holds the boot state; the generated grub.cfg does:

- `load_env` the state (`ORDER`, `a_OK`, `a_TRY`, `b_OK`, `b_TRY`).
- Walk `ORDER`; pick the first slot that is **confirmed good** (`_OK=1`) or
  **not yet tried** (`_TRY=0`).
- Mark that slot tried (`_TRY=1`) and `save_env` it back to the ESP.
- Boot `root=LABEL=rootfs-<slot> rauc.slot=<slot>`.

A freshly built image ships `ORDER="a b" a_OK=1 a_TRY=0 b_OK=0 b_TRY=0`, so it
boots slot A. **Rollback:** a slot that boots but is never confirmed (`_OK`
stays 0) has `_TRY=1` on the next boot and is skipped — GRUB falls back to the
other slot automatically.

## Manual update + rollback

Write the new rootfs to the inactive slot, point boot at it, reboot:

```sh
# on the device, after writing the new image to rootfs-b:
grub-editenv /boot/efi/EFI/osb/grubenv set ORDER="b a" b_OK=0 b_TRY=0
reboot
# after the new slot comes up healthy, confirm it so it stops being "on trial":
grub-editenv /boot/efi/EFI/osb/grubenv set b_OK=1 b_TRY=0
```

If slot B fails to confirm, the next boot rolls back to A with no action.

## RAUC

RAUC's GRUB backend uses exactly these variables. Point its `system.conf` at the
two slots and the grubenv:

```ini
[system]
compatible=my-osb-device
bootloader=grub
grubenv=/boot/efi/EFI/osb/grubenv

[slot.rootfs.0]
device=/dev/disk/by-partlabel/rootfs-a
type=ext4
bootname=a

[slot.rootfs.1]
device=/dev/disk/by-partlabel/rootfs-b
type=ext4
bootname=b
```

`rauc install bundle.raucb` writes the inactive slot and sets `ORDER`/`_TRY`;
`rauc status mark-good` sets `_OK`. The kernel's `rauc.slot=` (set by grub.cfg)
tells RAUC which slot booted.

## SWUpdate

SWUpdate targets the inactive partition and sets the same grubenv variables via
its `bootloader = "grub"` backend (`GRUBENV_PATH=/boot/efi/EFI/osb/grubenv`); its
`sw-description` selects the target slot by partition label. The boot selection
and rollback are identical to the RAUC flow above.

## Status / limits

- Non-Secure-Boot UEFI (GRUB) A/B is implemented and validated in QEMU.
- Secure Boot + A/B (dual signed UKIs per slot) is a follow-up.
- The on-device update client is provided by RAUC/SWUpdate; osb builds the
  A/B-capable image and the bootloader state they drive.
