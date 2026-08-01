# On-device installer

`osb flash` writes a prebuilt image from a build host onto a device you name.
The installer is the other half: it boots on the target machine and asks what
to do with it — which disk, encrypted or not, hostname, accounts — then
provisions the disk.

```sh
osb build -machine x86_64 installer-image
osb flash installer-image /dev/sdX       # write the live stick
# boot the target from it, then on that machine:
osb-installer
```

## Plan / execute split

The installer's core is `internal/installer`. `Plan(Request) ([]Step, error)`
is **pure**: it turns an answered request into the exact sequence of commands
and file writes that provisions the disk, and executes nothing. `Execute` walks
that sequence through a `Runner`.

This split is the design's whole point. An installer is the one program in osb
that cannot be exercised in CI — it needs a spare disk to destroy — so the part
that decides *what to do* is separated from the part that does it, and the
decision half is unit-tested exhaustively. `go test ./internal/installer`
asserts on exact argv:

```go
mkfs, _ := findStep(steps, "mkfs.ext4")
// "mkfs.ext4 -F -L rootfs /dev/sda2"
```

A wrong argv here erases somebody's disk, so the tests pin the commands rather
than checking that nothing errored. Three Runners share the interface:
`ExecRunner` (real), `DryRunner` (`-dry-run`, prints the plan), and the test
recorder.

`Step` is either a command (`Argv`, optional `Stdin`) or a file write
(`WritePath`, `Content`, `Mode`). Secrets — the LUKS passphrase, `chpasswd`
lines — travel on `Stdin` and never in `Argv`, because argv is world-readable
through `/proc`. Two tests assert exactly that.

## Layout

```
UEFI:   p1 ESP  (FAT32, 64 MiB, label ESP)     p2 root (ext4, label rootfs)
BIOS:   p1 root (ext4, label rootfs)            — MBR, no ESP
```

`fstab` addresses root by `LABEL=`, never by device node: the disk that is
`/dev/sda` under the installer may be `/dev/nvme0n1` on the next boot.
`PartitionDevice` handles the NVMe/mmcblk `p` separator — `/dev/nvme0n1p1`, not
`/dev/nvme0n11`.

## Encryption

`Encrypt` puts LUKS2 on the root partition and the ext4 on
`/dev/mapper/cryptroot`. The installer then has to solve the bootstrap problem:
**limine cannot read a LUKS container**, so the kernel and initramfs are staged
onto the unencrypted ESP and `limine.conf` addresses them through `boot():`
instead of `fslabel(rootfs):`. The cmdline carries
`cryptroot=PARTLABEL=rootfs cryptdm=cryptroot root=/dev/mapper/cryptroot`, and
`/etc/mkinitfs/mkinitfs.conf` gains the `cryptsetup` feature so the initramfs
can actually open the container.

**Encryption requires UEFI.** A BIOS layout has no ESP, so limine's stage 2,
its config, the kernel and the initramfs would all have to live inside the
container that stage 1 cannot read. `Validate` rejects the combination
(`ErrBIOSEncrypt`) rather than producing a disk that never boots.

Unlocking is by passphrase. TPM2 auto-unlock is not implemented.

## Secure Boot

`SecureBoot` copies the signed UKI the live medium already carries to the
target's `EFI/BOOT/BOOTX64.EFI` and writes no bootloader config — the cmdline
is inside the signature, so nothing here may alter it.

It does **not** enrol keys into firmware. The machine must already trust the
signing key; on hardware in Setup Mode you still need `osb key secure-boot` and
a manual enrolment. Combining Secure Boot with limine is refused at the machine
level for the reasons in the README.

## Accounts

Passwords are piped to `chpasswd` in the target chroot. A request with neither
a root password nor a user account is rejected (`ErrNoPassword`) — it would
produce a machine nobody can log into. A request with a user but no root
password locks root rather than leaving it passwordless.

Hostnames and usernames are validated against strict character sets before
anything is written, since both end up in files that a stray `\n` or `:` would
corrupt.

## Failure

There is no rollback. Once partitioning has begun the previous contents are
gone, so unwinding would restore nothing. A failed step aborts the sequence,
reports which step stopped it, and says plainly that the target is
half-installed; `/tmp/osb-installer.log` has the command output. Re-running the
installer from the start is the recovery path.

## Unattended installs

```sh
osb-installer -config install.conf
```

```ini
disk=/dev/nvme0n1
firmware=uefi
encrypt=yes
passphrase=...
hostname=node-01
username=ops
user_password=...
root_password=...
```

Unknown keys are an error, not a silent no-op: a typo'd `encrypt` would
otherwise ship a fleet of unencrypted machines.

## Current limits

- **Alpine only.** The generated fstab, crypttab, initramfs config and limine
  paths follow Alpine/mkinitfs conventions. A Debian/Ubuntu target needs a
  dracut/initramfs-tools variant of `configureSteps`.
- **No custom partitioning.** The layout is fixed (ESP + root, or root alone).
  There is no partition editor, no separate `/home`, no LVM, and no
  install-alongside — the installer takes the whole disk.
- **No squashfs/overlay live medium.** The stick boots its ext4 root
  read-write, so treat it as writable media.
- **`osb-installer` unit tracks `main`.** The repository has no tagged
  releases, and the unit builds from the published repo — so
  `osb build installer-image` only succeeds once these commits are on `main`.
  Pin `tag = "vX.Y.Z"` as soon as a release exists.
- **Not boot-tested.** The planning logic is unit-tested and the image
  resolves, but no installed disk has been booted; that needs KVM or hardware.
