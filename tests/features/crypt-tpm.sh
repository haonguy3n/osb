# /data is LUKS2 whose key is sealed to the TPM (features: tpm, encrypt).
# The first boot formats the volume and seals a random key to swtpm's PCRs, so
# everything here is asserted against the running system, not the build host.
# Expects tests/features/lib.sh to be prepended (see the header there).
data_device() {
    cryptsetup status data 2>/dev/null | sed -n 's/^[[:space:]]*device:[[:space:]]*//p'
}

tpm_device_present() { [ -e /dev/tpmrm0 ] || [ -e /dev/tpm0 ]; }

data_mapper_active() { [ -b /dev/mapper/data ]; }

data_is_luks2() { cryptsetup status data 2>/dev/null | grep -q 'LUKS2'; }

data_mounted_rw() { awk '$2 == "/data" {print $4}' /proc/mounts | grep -q '^rw'; }

data_writable() {
    p=/data/.osb-feature-probe
    : > "$p" && rm -f "$p"
}

# The key must live on the LUKS header as a token, not in a keyfile on the root
# filesystem: that is what makes the volume unlockable only through this TPM.
tpm_sealed_token() {
    dev=$(data_device)
    [ -n "$dev" ] || { echo "no device behind /dev/mapper/data"; return 1; }
    cryptsetup luksDump "$dev" 2>/dev/null | grep -q 'osb-tpm2'
}

echo "$(uname -srm) on $(hostname)"
check "TPM device is present" tpm_device_present
check "kernel command line has osb.crypt=" cmdline_has_prefix osb.crypt=
check "data mapper is active" data_mapper_active
check "data backing device is LUKS2" data_is_luks2
check "data carries the TPM-sealed key token" tpm_sealed_token
check "data is mounted read-write" data_mounted_rw
check "data is writable" data_writable
check "no failed services" no_failed_services
finish
