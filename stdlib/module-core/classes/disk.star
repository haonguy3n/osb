load("//classes/layout.star", "boot_partition")

_GUID = {
    "esp": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
    "boot": "BC13C2FF-59E6-4262-A352-B275FD6F7172",
    "data": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
    "raw": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
    "swap": "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F",
    "root/x86_64": "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709",
    "root/arm64": "B921B045-1DF0-41C3-AF44-4C6F280D3FAE",
    "verity/x86_64": "2C7357ED-EBD2-46D9-AEC1-23D437EC2BF5",
    "verity/arm64": "DF3300CE-D69F-4C92-978C-9BFB0F38D820",
}

_MBR = {"esp": "ef", "boot": "c", "swap": "82"}

def _ptype(p, table):
    if p["type"]:
        return p["type"]
    if table == "dos":
        return _MBR.get(p["role"], "83")
    return _GUID.get(p["role"]) or _GUID[p["role"] + "/" + ctx.arch]

def _layout_tsv(parts, table):
    rows = []
    for p in parts:
        cols = [p["name"], p["fs"], p["size"], p["role"], p["mount"], p["slot"],
                "1" if p["grow"] else "0", "1" if p["encrypt"] else "0",
                p["source"], _ptype(p, table), p["offset"]]
        rows.append("\t".join([c if c else "-" for c in cols]))
    return "\n".join(rows) + "\n"

_ASSEMBLE = r"""
set -euo pipefail
W=$DESTDIR/parts
R=$DESTDIR/rootfs
mkdir -p "$W"
export E2FSPROGS_FAKE_TIME=${SOURCE_DATE_EPOCH:-0}

uuid() { printf '%s' "$NAME/$1" | sha256sum | sed -E 's/^(.{8})(.{4})(.{4})(.{4})(.{12}).*/\1-\2-\3-\4-\5/'; }
mib() { case "$1" in
    *K) echo $(( (${1%K} + 1023) / 1024 )) ;;
    *M) echo "${1%M}" ;;
    *G) echo $(( ${1%G} * 1024 )) ;;
    *) echo "$1" ;;
  esac; }
sectors() { case "$1" in
    *K) echo $(( ${1%K} * 2 )) ;;
    *M) echo $(( ${1%M} * 2048 )) ;;
    *G) echo $(( ${1%G} * 2097152 )) ;;
    *) echo $(( $1 / 512 )) ;;
  esac; }
used_mb() { [ -d "$1" ] && du -sk "$1" | awk '{print int(($1 + 1023) / 1024)}' || echo 0; }

restore() {
  while IFS=$'\t' read -r name fs size role mount rest; do
    case "$mount" in -|/) continue ;; esac
    [ -d "$W/$name.orig" ] || continue
    find "$W/$name.orig" -mindepth 1 -maxdepth 1 -exec mv -t "$R$mount/" {} + 2>/dev/null || true
  done < "$DESTDIR/layout.tsv"
  rm -rf "$W"
}
trap restore EXIT

while IFS=$'\t' read -r name fs size role mount rest; do
  mkdir -p "$W/$name.d"
  case "$mount" in -|/) continue ;; esac
  mkdir -p "$W/$name.orig" "$R$mount"
  find "$R$mount" -mindepth 1 -maxdepth 1 -exec mv -t "$W/$name.orig/" {} +
  cp -a "$W/$name.orig/." "$W/$name.d/"
done < "$DESTDIR/layout.tsv"
cp -a "$DESTDIR/bootfs/." "$W/$BOOTPART.d/"

ROOT_MB=$(( $(used_mb "$R") * 13 / 10 + 64 ))
ROOT_MB=$(( (ROOT_MB + 3) / 4 * 4 ))

TABLE_FILE=$W/table.sfdisk
if [ "$TABLE" = gpt ]; then
  printf 'label: gpt\nlabel-id: %s\nunit: sectors\n' "$(uuid disk)" > "$TABLE_FILE"
else
  printf 'label: dos\nlabel-id: 0x%s\nunit: sectors\n' "$(printf '%s' "$NAME" | sha256sum | cut -c1-8)" > "$TABLE_FILE"
fi
: > "$W/plan"
next=2048
while IFS=$'\t' read -r name fs size role mount slot grow enc src type off; do
  content=$W/$name.d
  [ "$mount" = / ] && content=$R
  case "$size" in
    auto)
      case "$fs" in
        ext4) if [ "$role" = root ]; then mb=$ROOT_MB; else mb=$(( $(used_mb "$content") * 13 / 10 + 64 )); fi ;;
        vfat) mb=$(( $(used_mb "$content") * 3 / 2 + 32 )); [ $mb -lt 64 ] && mb=64 ;;
        verity) mb=$(( ROOT_MB / 64 + 2 )) ;;
        swap) mb=256 ;;
        raw) if [ "$src" != - ]; then mb=$(( ($(stat -c %s "$R$src") + 1048575) / 1048576 )); else mb=1; fi ;;
      esac ;;
    *) mb=$(mib "$size") ;;
  esac
  start=$next
  if [ "$off" != - ]; then start=$(sectors "$off"); fi
  sectors=$(( mb * 2048 ))
  next=$(( (start + sectors + 2047) / 2048 * 2048 ))
  echo "$name $fs $role $start $mb $enc $src" >> "$W/plan"
  if [ "$TABLE" = gpt ]; then
    echo "start=$start, size=$sectors, type=$type, uuid=$(uuid "part/$name"), name=\"$name\"" >> "$TABLE_FILE"
  else
    boot=""; [ "$role" = boot ] && boot=", bootable"
    echo "start=$start, size=$sectors, type=$type$boot" >> "$TABLE_FILE"
  fi
done < "$DESTDIR/layout.tsv"

TOTAL=$(( next + 2048 ))
truncate -s $(( TOTAL * 512 )) "$IMG"
sfdisk -q --no-reread --no-tell-kernel "$IMG" < "$TABLE_FILE"

while read -r name fs role start mb enc src; do
  f=$W/$name.img
  rm -f "$f"; truncate -s "${mb}M" "$f"
  content=$W/$name.d
  [ "$role" = root ] && content=$R
  case "$fs" in
    ext4)
      if [ "$enc" = 1 ]; then
        :
      elif [ "$role" = root ] && [ -n "$CONTENT_SLOT" ] && [ "$name" != "$CONTENT_SLOT" ]; then
        mkfs.ext4 -q -F -L "$name" -U "$(uuid "fs/$name")" -E hash_seed="$(uuid "seed/$name")" "$f"
      else
        opts=""
        [ "$role" = root ] && [ "$VERITY" = 1 ] && opts="-O ^has_journal"
        mkfs.ext4 -q -F $opts -L "$name" -U "$(uuid "fs/$name")" -E hash_seed="$(uuid "seed/$name"),lazy_itable_init=0,lazy_journal_init=0" -d "$content" "$f"
      fi ;;
    vfat)
      label=$(printf '%s' "$name" | tr a-z A-Z | cut -c1-11)
      mkfs.vfat -n "$label" -i "$(uuid "fs/$name" | cut -c1-8)" --invariant "$f" >/dev/null
      if [ -n "$(ls -A "$content" 2>/dev/null)" ]; then
        mcopy -s -m -Q -i "$f" "$content"/* ::/
      fi ;;
    swap) mkswap -L "$name" -U "$(uuid "fs/$name")" "$f" >/dev/null ;;
    raw) [ "$src" != - ] && dd if="$R$src" of="$f" conv=notrunc status=none ;;
    verity) : ;;
  esac
  dd if="$f" of="$IMG" bs=4M seek=$(( start * 512 )) oflag=seek_bytes conv=notrunc,sparse status=none
  rm -f "$f"
done < "$W/plan"
echo "disk: $(basename "$IMG") $(( TOTAL / 2048 )) MiB, $TABLE"
column -t "$W/plan" 2>/dev/null || cat "$W/plan"
"""

def assemble_disk(name, parts, table, verity, content_slot):
    tsv = _layout_tsv(parts, table)
    env = 'NAME=%s IMG=$DESTDIR/%s.img TABLE=%s BOOTPART=%s VERITY=%s CONTENT_SLOT=%s' % (
        name, name, table, boot_partition(parts)["name"], "1" if verity else "0", content_slot)
    run("cat > $DESTDIR/layout.tsv <<'OSB_EOF'\n" + tsv + "OSB_EOF\nrm -f $DESTDIR/%s.img && touch $DESTDIR/%s.img" % (name, name))
    run(env + " bash -c " + _shell_quote(_ASSEMBLE), privileged = True)

def _shell_quote(s):
    return "'" + s.replace("'", "'\"'\"'") + "'"

_STAGE = r"""
set -euo pipefail
R=$DESTDIR/rootfs
B=$DESTDIR/bootfs
rm -rf "$B" "$DESTDIR/uki"
mkdir -p "$B"
KERNEL=$(ls -1 "$R"/boot/vmlinuz* 2>/dev/null | grep -v '\.old$' | sort -V | tail -1)
INITRD=$R/boot/osb-initrd.img
[ -n "$KERNEL" ] || { echo "no kernel in $R/boot - add a kernel package or set kernel on the machine" >&2; exit 1; }
[ -f "$INITRD" ] || { echo "no $INITRD - the osb-initrd package must be in the image" >&2; exit 1; }
case "$ARCH" in arm64) EFI=BOOTAA64.EFI; EFIARCH=aa64; GRUBFMT=arm64-efi ;; *) EFI=BOOTX64.EFI; EFIARCH=x64; GRUBFMT=x86_64-efi ;; esac
"""

def stage_boot(loader, firmware, entries, timeout):
    script = _STAGE
    if loader == "uki":
        script += r"""
mkdir -p "$B/EFI/BOOT" "$B/EFI/osb" "$DESTDIR/uki"
cp "$KERNEL" "$DESTDIR/uki/vmlinuz"
cp "$INITRD" "$DESTDIR/uki/initrd.img"
for s in "$R/usr/lib/systemd/boot/efi/linux$EFIARCH.efi.stub" "$R/usr/lib/gummiboot/linux$EFIARCH.efi.stub"; do
  if [ -f "$s" ]; then cp "$s" "$DESTDIR/uki/stub.efi"; break; fi
done
chmod -R a+rX "$DESTDIR/uki"
"""
        run(script, privileged = True)
        return

    for e in entries:
        script += 'mkdir -p "$B/osb/%s" && cp "$KERNEL" "$B/osb/%s/vmlinuz" && cp "$INITRD" "$B/osb/%s/initrd.img"\n' % (e["slot"], e["slot"], e["slot"])

    if loader == "limine":
        conf = ["timeout: %d" % timeout, "serial: yes", "default_entry: 1", ""]
        for e in entries:
            conf += [
                "/osb%s" % (" (slot %s)" % e["slot"] if len(entries) > 1 else ""),
                "    protocol: linux",
                "    path: boot():/osb/%s/vmlinuz" % e["slot"],
                "    cmdline: %s" % e["cmdline"],
                "    module_path: boot():/osb/%s/initrd.img" % e["slot"],
                "",
            ]
        script += "cat > $B/limine.conf <<'OSB_EOF'\n" + "\n".join(conf) + "OSB_EOF\n"
        script += r"""
L=$R/usr/share/limine
[ -d "$L" ] || { echo "limine is not installed in the rootfs" >&2; exit 1; }
"""
        if firmware == "bios":
            script += 'cp "$L/limine-bios.sys" "$B/"\n'
        else:
            script += 'mkdir -p "$B/EFI/BOOT" && cp "$L/$EFI" "$B/EFI/BOOT/$EFI"\n'
        run(script, privileged = True)
        return

    cfg = ["set timeout=%d" % timeout, "set default=0"]
    if len(entries) == 1:
        e = entries[0]
        cfg += [
            'menuentry "osb" {',
            "  linux /osb/%s/vmlinuz %s" % (e["slot"], e["cmdline"]),
            "  initrd /osb/%s/initrd.img" % e["slot"],
            "}",
        ]
    else:
        slots = [e["slot"] for e in entries]
        cfg += ['set ORDER="%s"' % " ".join(slots)]
        cfg += ["set %s_OK=0\nset %s_TRY=0" % (s, s) for s in slots]
        cfg += ["load_env -f /EFI/osb/grubenv", "set target=", "for slot in $ORDER; do"]
        for s in slots:
            cfg += ['  if [ "$slot" = "%s" ]; then set OK=$%s_OK; set TRY=$%s_TRY; fi' % (s, s, s)]
        cfg += ['  if [ "$OK" = "1" -o "$TRY" = "0" ]; then', "    set target=$slot"]
        for s in slots:
            cfg += ['    if [ "$slot" = "%s" ]; then set %s_TRY=1; fi' % (s, s)]
        cfg += ["    break", "  fi", "done", 'if [ -z "$target" ]; then set target=%s; fi' % slots[0]]
        cfg += ["save_env -f /EFI/osb/grubenv " + " ".join(["%s_TRY" % s for s in slots])]
        for e in entries:
            cfg += [
                'if [ "$target" = "%s" ]; then' % e["slot"],
                "  linux /osb/%s/vmlinuz %s" % (e["slot"], e["cmdline"]),
                "  initrd /osb/%s/initrd.img" % e["slot"],
                "fi",
            ]
        cfg += ["boot"]
        env = ['ORDER="%s"' % " ".join(slots)]
        for i, s in enumerate(slots):
            env += ["%s_OK=%d" % (s, 1 if i == 0 else 0), "%s_TRY=0" % s]
        script += 'mkdir -p "$B/EFI/osb"\nchroot "$R" grub-editenv /tmp/osb-grubenv create\n'
        script += "chroot \"$R\" grub-editenv /tmp/osb-grubenv set %s\n" % " ".join(env)
        script += 'mv "$R/tmp/osb-grubenv" "$B/EFI/osb/grubenv"\n'
    script += "mkdir -p $B/EFI/BOOT\ncat > $B/EFI/BOOT/grub.cfg <<'OSB_EOF'\n" + "\n".join(cfg) + "\nOSB_EOF\n"
    script += r"""
chroot "$R" grub-mkimage -O $GRUBFMT -o /tmp/osb-grub.efi -p /EFI/BOOT \
  part_gpt part_msdos fat ext2 normal linux configfile search search_label search_fs_uuid \
  echo ls test loadenv reboot halt gzio
mv "$R/tmp/osb-grub.efi" "$B/EFI/BOOT/$EFI"
"""
    run(script, privileged = True)

def install_bios_loader(name):
    run("""
set -e
mkdir -p $DESTDIR/rootfs/mnt
mount --bind $DESTDIR $DESTDIR/rootfs/mnt
trap 'umount $DESTDIR/rootfs/mnt' EXIT
chroot $DESTDIR/rootfs /usr/bin/limine bios-install /mnt/%s
""" % name, privileged = True)

def make_iso(name, cmdline):
    conf = "\n".join([
        "timeout: 5",
        "serial: yes",
        "",
        "/Install %s" % name,
        "    protocol: linux",
        "    path: boot():/osb/vmlinuz",
        "    cmdline: %s osb.install=/osb/%s.img.gz" % (cmdline, name),
        "    module_path: boot():/osb/initrd.img",
        "",
    ])
    run(r"""
set -euo pipefail
R=$DESTDIR/rootfs
L=$R/usr/share/limine
I=$DESTDIR/iso_root
[ -d "$L" ] || { echo "iso: limine is not installed in the rootfs" >&2; exit 1; }
rm -rf "$I"
mkdir -p "$I/boot/limine" "$I/EFI/BOOT" "$I/osb"
cp "$(ls -1 "$R"/boot/vmlinuz* | sort -V | tail -1)" "$I/osb/vmlinuz"
cp "$R/boot/osb-initrd.img" "$I/osb/initrd.img"
gzip -1 -c "$DESTDIR/NAME.img" > "$I/osb/NAME.img.gz"
stat -c %s "$DESTDIR/NAME.img" > "$I/osb/NAME.size"
cat > "$I/boot/limine/limine.conf" <<'OSB_EOF'
CONF
OSB_EOF
cp "$L/limine-uefi-cd.bin" "$I/boot/limine/"
if [ "$ARCH" = x86_64 ]; then
  cp "$L/limine-bios-cd.bin" "$L/limine-bios.sys" "$I/boot/limine/"
  cp "$L/BOOTX64.EFI" "$I/EFI/BOOT/"
  xorriso -as mkisofs -quiet -R -r -J -V OSB_INSTALL \
    -b boot/limine/limine-bios-cd.bin -no-emul-boot -boot-load-size 4 -boot-info-table \
    --efi-boot boot/limine/limine-uefi-cd.bin -efi-boot-part --efi-boot-image \
    --protective-msdos-label "$I" -o "$DESTDIR/NAME.iso"
  mkdir -p "$R/mnt"
  mount --bind "$DESTDIR" "$R/mnt"
  trap 'umount "$R/mnt"' EXIT
  chroot "$R" /usr/bin/limine bios-install /mnt/NAME.iso
else
  cp "$L/BOOTAA64.EFI" "$I/EFI/BOOT/"
  xorriso -as mkisofs -quiet -R -r -J -V OSB_INSTALL \
    --efi-boot boot/limine/limine-uefi-cd.bin -efi-boot-part --efi-boot-image \
    --protective-msdos-label "$I" -o "$DESTDIR/NAME.iso"
fi
rm -rf "$I"
echo "iso: NAME.iso"
""".replace("NAME", name).replace("CONF", conf), privileged = True)
