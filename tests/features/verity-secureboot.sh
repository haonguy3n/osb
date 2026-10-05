# dm-verity root behind a Secure Boot signed UKI (features: secureboot, verity).
# Expects tests/features/lib.sh to be prepended (see the header there).
verity_active() {
    command -v veritysetup >/dev/null 2>&1 || return 1
    veritysetup status root 2>/dev/null | grep -qi 'is active'
}

root_device_is_verity() {
    command -v dmsetup >/dev/null 2>&1 || return 1
    dmsetup table root 2>/dev/null | grep -q 'verity'
}

# The hash dm-verity checks against has to be the one the *signed* kernel
# command line carries - that pairing is what makes verity meaningful, and it is
# also what proves the UKI was built with this root filesystem's hash.
verity_hash_matches_cmdline() {
    want=$(cmdline_value roothash | tr -d '[:space:]')
    [ -n "$want" ] || { echo "no roothash= on the command line"; return 1; }
    status=$(veritysetup status root 2>&1)
    # cryptsetup spells this "  root hash:" in status output and "Root hash:" in
    # format output; accept either rather than depend on the casing.
    got=$(printf '%s\n' "$status" | sed -n 's/^[[:space:]]*[Rr]oot hash:[[:space:]]*//p' | tr -d '[:space:]')
    [ -n "$got" ] || { echo "$status"; return 1; }
    [ "$want" = "$got" ] || {
        echo "command line: roothash=$want"
        echo "verity device: root hash=$got"
        return 1
    }
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
check "no failed services" no_failed_services
finish
