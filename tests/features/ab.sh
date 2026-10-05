# A/B layout: two root slots, both bootable from the ESP, and a shared /data
# (feature: ab).
#
# The ESP is mounted at /boot/efi, so the staged slot kernels, the GRUB script
# that chooses between them and the grubenv it reads are all inspectable from
# the running guest.
#
# What this does NOT cover: actually falling back to slot b. GRUB's script
# picks the first slot whose <slot>_OK is 1 or whose <slot>_TRY is 0, but
# nothing in the guest ever marks a booted slot OK or clears TRY (the flags are
# only seeded at image build time), so a slot that stops booting is retried
# rather than skipped. Testing that needs a second boot against a deliberately
# broken slot and lands with the OTA work.
# Expects tests/features/lib.sh to be prepended (see the header there).
partlabel_exists() { [ -e "/dev/disk/by-partlabel/$1" ]; }

both_slots_present() { partlabel_exists root-a && partlabel_exists root-b; }

booted_from_slot_a() { cmdline_has osb.slot=a; }

root_is_slot_a() { cmdline_has root=PARTLABEL=root-a; }

data_mounted() { awk '$2 == "/data"' /proc/mounts | grep -q .; }

esp_mounted() { awk '$2 == "/boot/efi"' /proc/mounts | grep -q .; }

# Both slots must carry a kernel and an initramfs, otherwise GRUB has nothing
# to fall back to.
both_slots_staged_on_esp() {
    for d in a b; do
        [ -f "/boot/efi/osb/$d/vmlinuz" ] || { echo "/boot/efi/osb/$d/vmlinuz missing"; return 1; }
        [ -f "/boot/efi/osb/$d/initrd.img" ] || { echo "/boot/efi/osb/$d/initrd.img missing"; return 1; }
    done
}

# The GRUB script has to offer both slots and keep its state in the grubenv.
grub_config_covers_both_slots() {
    cfg=/boot/efi/EFI/BOOT/grub.cfg
    [ -f "$cfg" ] || { echo "$cfg missing"; return 1; }
    grep -q 'ORDER="a b"' "$cfg" || { echo "no ORDER listing both slots"; return 1; }
    for d in a b; do
        grep -q "/osb/$d/vmlinuz" "$cfg" || { echo "no boot entry for slot $d"; return 1; }
    done
}

grubenv_present() { [ -f /boot/efi/EFI/osb/grubenv ]; }

echo "$(uname -srm) on $(hostname)"
check "kernel command line has osb.slot=a" booted_from_slot_a
check "root is PARTLABEL=root-a" root_is_slot_a
check "both root slots exist on the disk" both_slots_present
check "ESP is mounted" esp_mounted
check "both slots are staged on the ESP" both_slots_staged_on_esp
check "GRUB config covers both slots" grub_config_covers_both_slots
check "GRUB environment is present" grubenv_present
check "/data is mounted" data_mounted
check "no failed services" no_failed_services
finish
