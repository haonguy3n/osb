# Shared helpers for the feature tests in tests/features/*.sh.
#
# The workflow concatenates this file with a case script and pipes the result
# into the guest, so a case script can assume these are defined:
#
#   cat tests/features/lib.sh tests/features/<case>.sh > "$RUNNER_TEMP/feature.sh"
#   osb run -test "$RUNNER_TEMP/feature.sh" <image>
#
# Keep everything POSIX sh: the guest may be Alpine (busybox ash) or an apt
# distro (dash).
failed=0

check() {
    name=$1
    shift
    if out=$("$@" 2>&1); then
        echo "ok   $name"
    else
        echo "FAIL $name"
        [ -n "$out" ] && echo "$out" | sed 's/^/     /'
        failed=1
    fi
}

cmdline_has() { tr ' ' '\n' < /proc/cmdline | grep -qx "$1"; }
cmdline_has_prefix() { tr ' ' '\n' < /proc/cmdline | grep -q "^$1"; }
cmdline_value() { tr ' ' '\n' < /proc/cmdline | sed -n "s/^$1=//p"; }

# efivarfs stores four attribute bytes before the value.
efi_var_byte() {
    [ -e "$1" ] || return 1
    dd if="$1" bs=1 skip=4 count=1 2>/dev/null | od -An -t u1 | tr -d ' '
}

secureboot_enforced() {
    [ "$(efi_var_byte /sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c)" = "1" ]
}

root_is_overlay() { awk '$2 == "/" && $3 == "overlay"' /proc/mounts | grep -q .; }
lower_root_is_readonly() { awk '$2 ~ /osb\/lower$/ {print $4}' /proc/mounts | grep -q '^ro'; }
upper_root_is_tmpfs() { awk '$2 == "/run/osb/rw" {print $3}' /proc/mounts | grep -qx tmpfs; }

# The root filesystem is writable through the overlay, even when the lower
# device is read-only.
root_writable() {
    p=/osb-feature-probe
    : > "$p" && rm -f "$p"
}

no_failed_services() {
    if command -v systemctl >/dev/null 2>&1; then
        systemctl is-system-running --wait >/dev/null
        failed_units=$(systemctl --failed --no-legend --plain)
    else
        failed_units=$(rc-status --crashed)
    fi
    echo "$failed_units"
    [ -z "$failed_units" ]
}

finish() {
    if [ "$failed" = 0 ]; then
        echo "✅ feature test: PASS"
    else
        echo "❌ feature test: FAIL"
    fi
    exit $failed
}
