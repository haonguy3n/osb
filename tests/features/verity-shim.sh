# dm-verity root behind osb's own signed UKI, with the distro's shim as the first
# stage (features: secureboot, verity; bootloader: shim).
#
# The guest only comes up at all if shim verified our UKI, so the verity
# assertions below are also the proof that the chain held. Expects
# tests/features/lib.sh to be prepended (see the header there).
verity_active() {
    command -v veritysetup >/dev/null 2>&1 || return 1
    veritysetup status root 2>/dev/null | grep -qi 'is active'
}

root_device_is_verity() {
    command -v dmsetup >/dev/null 2>&1 || return 1
    dmsetup table root 2>/dev/null | grep -q 'verity'
}

# The hash dm-verity checks against has to be the one the *signed* command line
# carries - that pairing is what makes verity meaningful here.
verity_hash_matches_cmdline() {
    want=$(cmdline_value roothash | tr -d '[:space:]')
    [ -n "$want" ] || { echo "no roothash= on the command line"; return 1; }
    status=$(veritysetup status root 2>&1)
    got=$(printf '%s\n' "$status" | sed -n 's/^[[:space:]]*[Rr]oot hash:[[:space:]]*//p' | tr -d '[:space:]')
    [ -n "$got" ] || { echo "$status"; return 1; }
    [ "$want" = "$got" ] || {
        echo "command line: roothash=$want"
        echo "verity device: root hash=$got"
        return 1
    }
}

# The first stage is shim, and the second stage - the file shim loads by name -
# is our UKI, which carries the verity command line inside it.
esp_has_shim_then_our_uki() {
    [ -f /boot/efi/EFI/BOOT/BOOTX64.EFI ] || { echo "no EFI/BOOT/BOOTX64.EFI"; return 1; }
    strings /boot/efi/EFI/BOOT/BOOTX64.EFI 2>/dev/null | grep -qi 'UEFI SHIM' || {
        echo "the default boot file is not shim"; return 1
    }
    [ -f /boot/efi/EFI/BOOT/grubx64.efi ] || { echo "no second stage grubx64.efi"; return 1; }
    grep -qa 'roothash=' /boot/efi/EFI/BOOT/grubx64.efi || {
        echo "the second stage does not carry the verity command line"; return 1
    }
}

# The certificate MokManager/mokutil enrol is public material, and has to be
# where someone at the console can pick it.
esp_has_osb_certificate() {
    [ -s /boot/efi/EFI/osb/osb.crt ] || { echo "no EFI/osb/osb.crt on the ESP"; return 1; }
}

echo "$(uname -srm) on $(hostname)"
check "Secure Boot is enforced" secureboot_enforced
check "kernel command line has a roothash" cmdline_has_prefix roothash=
check "kernel command line has osb.overlay=tmpfs" cmdline_has osb.overlay=tmpfs
check "dm-verity target is active" verity_active
check "root device is a dm-verity mapping" root_device_is_verity
check "verity hash matches the signed command line" verity_hash_matches_cmdline
check "root is an overlay" root_is_overlay
check "verity lower root is read-only" lower_root_is_readonly
check "writes land in the overlay" root_writable
check "ESP has shim then our signed UKI" esp_has_shim_then_our_uki
check "ESP has osb's certificate for MOK" esp_has_osb_certificate
check "no failed services" no_failed_services
finish
