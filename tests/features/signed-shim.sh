# The distro's signed boot chain: shim -> signed GRUB -> signed kernel, with the
# firmware's own keys. What has to hold is that Secure Boot really is enforced
# (otherwise the loader proves nothing) and that the root still came up as the
# read-only overlay this image asked for.
# Expects tests/features/lib.sh to be prepended (see the header there).
esp_is_shim_then_grub() {
    # /boot/efi is mounted: shim as the default boot file, the signed GRUB next
    # to it, and the modules the config needs.
    [ -f /boot/efi/EFI/BOOT/BOOTX64.EFI ] || { echo "no EFI/BOOT/BOOTX64.EFI"; return 1; }
    [ -f /boot/efi/EFI/ubuntu/grubx64.efi ] || { echo "no EFI/ubuntu/grubx64.efi"; return 1; }
    [ -f /boot/efi/EFI/ubuntu/grub.cfg ] || { echo "no EFI/ubuntu/grub.cfg"; return 1; }
    ls /boot/efi/EFI/ubuntu/*-efi/*.mod >/dev/null 2>&1 || { echo "no grub modules on the ESP"; return 1; }
}

shim_is_first_stage() {
    # shim's own strings identify it; the kernel is the distro's signed one.
    strings /boot/efi/EFI/BOOT/BOOTX64.EFI 2>/dev/null | grep -qi 'shim' || return 1
}

echo "$(uname -srm) on $(hostname)"
check "Secure Boot is enforced" secureboot_enforced
check "kernel command line has osb.overlay=tmpfs" cmdline_has osb.overlay=tmpfs
check "root is an overlay" root_is_overlay
check "overlay lower is read-only" lower_root_is_readonly
check "ESP carries shim and the signed grub" esp_is_shim_then_grub
check "the first stage is shim" shim_is_first_stage
check "no failed services" no_failed_services
finish
