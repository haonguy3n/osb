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

# efivarfs stores four attribute bytes before the value and does not support
# seeking - `dd bs=1 skip=4` finds nothing to read - so take the value byte out
# of a sequential read instead.
efi_var_byte() {
    [ -e "$1" ] || return 1
    od -An -t u1 -N 5 "$1" 2>/dev/null | awk '{print $5}'
}

# Firmware state, dumped when the Secure Boot check fails: the raw variable (four
# attribute bytes, then the value) is what tells "the firmware never got our
# keys" apart from "the variable cannot be read this way".
secureboot_state() {
    efivars=/sys/firmware/efi/efivars
    v="$efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
    s="$efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c"
    echo "efivars:    $(ls "$efivars" 2>/dev/null | wc -l) variables"
    echo "SecureBoot: $([ -e "$v" ] && od -An -t x1 "$v" | tr -s ' ' || echo missing)"
    echo "SetupMode:  $([ -e "$s" ] && od -An -t x1 "$s" | tr -s ' ' || echo missing)"
    echo "lockdown:   $(cat /sys/kernel/security/lockdown 2>/dev/null)"
}

secureboot_enforced() {
    v=/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c
    [ -e "$v" ] || { secureboot_state; return 1; }
    byte=$(efi_var_byte "$v")
    [ "$byte" = "1" ] || { echo "SecureBoot value byte: ${byte:-<unreadable>}"; secureboot_state; return 1; }
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
        # Enough context to diagnose from the CI log alone: which device mapper
        # targets exist, what the kernel was told, how / is mounted and which
        # services are unhappy.
        echo "--- kernel command line ---"
        cat /proc/cmdline
        echo "--- mounts ---"
        cat /proc/mounts
        echo "--- device mapper ---"
        ls -l /dev/mapper 2>&1
        echo "--- failed services ---"
        if command -v systemctl >/dev/null 2>&1; then
            systemctl --failed --no-legend --plain
        else
            rc-status --crashed
        fi
    fi
    exit $failed
}
