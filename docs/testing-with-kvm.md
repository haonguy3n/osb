# Testing osb images with KVM

How to boot what osb builds on your own machine, without hardware.

## Host setup

KVM itself needs no configuration if `/dev/kvm` exists and is accessible:

```sh
lsmod | grep kvm          # kvm_intel or kvm_amd loaded
ls -l /dev/kvm            # readable/writable by you
```

`osb run` adds `-enable-kvm` automatically when `/dev/kvm` is present and the
image's arch matches the host. Without it QEMU falls back to TCG emulation,
which boots several times slower but still works.

Host packages (Arch/CachyOS names; Debian equivalents in parentheses):

```sh
sudo pacman -S --needed \
    qemu-system-x86 \      # (qemu-system-x86)
    edk2-ovmf \            # (ovmf)              UEFI + Secure Boot machines
    systemd-ukify \        # (systemd-ukify)     Secure Boot UKI signing
    virt-firmware \        # (python3-virt-firmware)
    mtools \               # (mtools)            ESP assembly
    libisoburn \           # (xorriso)           only if building ISOs on the host
    bmap-tools             # (bmap-tools)        faster flashing, optional
```

`osb run` names any missing package before it launches, so you can also just
run it and read the error.

**The `loop` module must be loaded.** osb assembles partition images inside
privileged containers that `mknod` their own `/dev/loopN`, which bypasses the
`loop-control` autoload trigger, so on-demand loading never fires:

```sh
sudo modprobe loop
echo loop | sudo tee /etc/modules-load.d/osb-loop.conf   # persist across reboots
```

A kernel upgrade replaces `/lib/modules/<running-kernel>`, so `modprobe` cannot
load anything new until you reboot. If `modprobe loop` reports "Module loop not
found", that is why.

## Booting a disk image

```sh
osb build base-image
osb run  base-image                  # serial console on stdout
osb run  base-image -display         # graphical window
osb run  base-image -m 8G            # override machine memory
```

Non-interactive smoke test - boots headless, waits for the login prompt, SSHes
in, runs a health check, powers off, and exits non-zero on any failure:

```sh
osb run ssh-image -boot-test
osb run ssh-image -boot-test -timeout 5m
```

Use an image that ships `openssh`. `base-image` has no sshd, so `-boot-test`
reaches the login prompt and then fails its SSH step.

## Booting an ISO

Images built with `iso = True` emit a `.iso` beside the `.img`. `osb run` boots
the `.img`, so drive the ISO with QEMU directly:

```sh
ISO=build/alpine/installer-image.<machine>/destdir/installer-image.iso

qemu-system-x86_64 -enable-kvm -M q35 -cpu host -m 4G \
    -cdrom "$ISO" -boot d \
    -nographic -display none -serial mon:stdio
```

Add a blank disk to install onto:

```sh
truncate -s 8G target.img

qemu-system-x86_64 -enable-kvm -M q35 -cpu host -m 4G \
    -cdrom "$ISO" -boot d \
    -drive file=target.img,format=raw,if=virtio \
    -nographic -display none -serial mon:stdio
```

Then inside the guest, log in as `root` and either run `osb-installer` and
answer the prompts, or drive it unattended:

```sh
cat > /tmp/install.conf <<'EOF'
disk=/dev/vda
hostname=osb-target
username=you
user_password=changeme
root_password=changeme
EOF

osb-installer -config /tmp/install.conf     # add -dry-run to see the plan only
```

To boot the result, run QEMU again with the same `-drive` and no `-cdrom`.

`-nographic` puts the guest's serial console on your terminal. Exit with
`Ctrl-a x`.

## UEFI and Secure Boot

```sh
osb build -machine qemu-x86_64-uefi base-image
osb run   -machine qemu-x86_64-uefi base-image

osb key secure-boot                                          # project keys
osb build -machine qemu-x86_64-uefi-secureboot base-image     # signs the UKI
osb run   -machine qemu-x86_64-uefi-secureboot base-image     # enforcing SB
```

The Secure Boot run builds a per-run OVMF variable store with osb's key
enrolled, then boots with the split CODE/VARS pflash form and SMM enabled. A
build signed with the bundled test key is not secure on real hardware -
`osb flash` warns about this - because that key is public in git.

## Inspecting a built image from the host

The rootfs is root-owned, so read it with `sudo`:

```sh
D=build/alpine/<image>.<machine>/destdir
sudo ls "$D/rootfs/etc"
sudo find "$D/rootfs" -name wipefs        # is a tool actually in the image?

LOOP=$(sudo losetup --find --show -P "$D/<image>.img")
sudo mount ${LOOP}p1 /mnt/x
...
sudo umount /mnt/x && sudo losetup -d $LOOP
```

`osb log <unit>` prints a unit's build log, and `osb shell` drops you into the
build container with the same environment a unit's tasks get - the fastest way
to reproduce a failing build step by hand.

## Known limitation: limine on a disk cannot boot

limine 12.5.2 ships **no ext2/ext4 driver**. Both its BIOS stages support only
FAT32 and ISO9660:

```sh
strings /usr/share/limine/limine-bios.sys | grep -ciE 'ext2|ext4'   # 0
```

The limine *machines* stage `limine-bios.sys`, `limine.conf`, the kernel and
the initramfs on an ext4 root and address the kernel as
`fslabel(rootfs):/boot/vmlinuz`. limine cannot read any of that, so a limine
disk image panics at boot:

```
!! Stage 3 file not found!
PANIC: Failed to load stage 3.
```

This affects `qemu-x86_64-limine` and, for the same reason, the kernel-loading
half of the UEFI limine machines - their `BOOTX64.EFI` and `limine.conf` live
on the FAT ESP and load fine, but the kernel does not. Fixing it means putting
everything limine reads on a FAT partition.

**ISO images are unaffected** and boot correctly, because ISO9660 is one of the
two filesystems limine does support. Use `syslinux` (the default for a non-ESP
layout) or the GRUB/UKI UEFI paths for disk images until this is resolved.
