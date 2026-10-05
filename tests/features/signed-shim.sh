# The distro's signed boot chain: shim -> signed GRUB -> signed kernel, with the
# firmware's own keys. What has to hold is that Secure Boot really is enforced
# (otherwise the loader proves nothing) and that the root still came up as the
# read-only overlay this image asked for.
# Expects tests/features/lib.sh to be prepended (see the header there).

# A minimal image has no `strings` (that is binutils), so match bytes with
# grep -a: shim's own banner is plain ASCII inside the PE.
first_stage_is_shim() {
    grep -aq 'UEFI SHIM' /boot/efi/EFI/BOOT/BOOTX64.EFI 2>/dev/null
}

esp_is_shim_then_signed_grub() {
    [ -f /boot/efi/EFI/BOOT/BOOTX64.EFI ] || { echo "no EFI/BOOT/BOOTX64.EFI"; return 1; }
    [ -f /boot/efi/EFI/ubuntu/grubx64.efi ] || { echo "no EFI/ubuntu/grubx64.efi"; return 1; }
    [ -f /boot/efi/EFI/ubuntu/grub.cfg ] || { echo "no EFI/ubuntu/grub.cfg"; return 1; }
    grep -aq 'osb.overlay=tmpfs' /boot/efi/EFI/ubuntu/grub.cfg || { echo "grub.cfg is not osb's"; return 1; }
}

# The firmware's db is what makes shim trustworthy in the first place. It is an
# EFI variable holding an EFI_SIGNATURE_LIST, so the certificate subjects are
# readable straight out of it - no efitools (a universe package) needed, and it
# proves the exact bytes the firmware would check.
firmware_db_has_vendor_keys() {
    db=/sys/firmware/efi/efivars/db-d719b2cb-3d3a-4596-a3bc-dad00e67656f
    [ -e "$db" ] || { echo "no db variable in efivarfs"; return 1; }
    tr -d '\0' < "$db" | grep -qa 'Microsoft' && return 0
    tr -d '\0' < "$db" | head -c 200 | od -An -c | head -6
    return 1
}

echo "$(uname -srm) on $(hostname)"
check "Secure Boot is enforced" secureboot_enforced
check "kernel command line has osb.overlay=tmpfs" cmdline_has osb.overlay=tmpfs
check "root is an overlay" root_is_overlay
check "overlay lower is read-only" lower_root_is_readonly
check "ESP carries shim and the signed grub" esp_is_shim_then_signed_grub
check "the first stage is shim" first_stage_is_shim
check "firmware db has the vendor keys" firmware_db_has_vendor_keys
check "no failed services" no_failed_services
finish
