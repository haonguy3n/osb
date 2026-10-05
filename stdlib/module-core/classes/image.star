load("//classes/exclusive.star", "assert_single", "select")
load("//classes/layout.star", "boot_partition", "check_layout", "default_layout", "root_partitions", "verity_partition")
load("//classes/disk.star", "assemble_disk", "install_bios_loader", "make_iso", "stage_boot")
load("//classes/users.star", "user")

FEATURES = ["secureboot", "verity", "readonly", "encrypt", "tpm", "ab"]
LOADERS = ["grub", "limine", "uki", "shim"]

_APT = ["debian", "ubuntu"]

_APT_ESSENTIAL = [
    "base-files", "base-passwd", "bash", "bsdutils", "coreutils", "dash",
    "debianutils", "diffutils", "dpkg", "findutils", "grep", "gzip", "hostname",
    "init-system-helpers", "kmod", "libc-bin", "login", "mawk", "passwd",
    "perl-base", "sed", "sysvinit-utils", "tar", "udev", "util-linux",
]

_PACKAGES = {
    "initrd": {"alpine": ["osb-initrd", "busybox", "kmod"], "apt": ["osb-initrd", "busybox-static", "kmod"]},
    "grub": {"alpine": ["grub", "grub-efi"], "apt/x86_64": ["grub-efi-amd64-bin"], "apt/arm64": ["grub-efi-arm64-bin"]},
    "limine": {"alpine": ["limine"], "apt": ["limine"]},
    # The distro's own signed chain: shim (signed by Microsoft's UEFI CA, so the
    # firmware trusts it) loading the distro's signed GRUB and signed kernel.
    # Boots under stock Secure Boot keys without osb signing anything.
    "shim": {
        "apt/x86_64": ["shim-signed", "grub-efi-amd64-signed", "grub-efi-amd64-bin"],
        "apt/arm64": ["shim-signed", "grub-efi-arm64-signed", "grub-efi-arm64-bin"],
    },
    "uki": {"alpine": ["gummiboot-efistub"], "apt": ["systemd-boot-efi"]},
    "verity": {"alpine": ["cryptsetup"], "apt": ["cryptsetup-bin", "dmsetup"]},
    "encrypt": {"alpine": ["cryptsetup", "e2fsprogs"], "apt": ["cryptsetup-bin", "dmsetup", "e2fsprogs"]},
    "tpm": {"alpine": ["tpm2-tools", "tpm2-tss-tcti-device"], "apt": ["tpm2-tools", "libtss2-tcti-device0t64"]},
    # Alpine ships resize2fs in e2fsprogs-extra, not e2fsprogs, so without it the
    # initrd cannot grow the root filesystem after the partition was extended.
    "grow": {"alpine": ["sfdisk", "e2fsprogs", "e2fsprogs-extra"], "apt": ["fdisk", "e2fsprogs"]},
}

def _family(distro):
    return "apt" if distro in _APT else distro

def _feature_packages(key, family):
    table = _PACKAGES[key]
    return table.get(family + "/" + ctx.arch, table.get(family, []))

def image(name, packages = [], distro_packages = {}, distro = None,
          bootloader = None, features = [], layout = None, cmdline = "",
          users = None, services = [], hostname = None, timezone = "",
          iso = False, init = None, tpm_pcrs = "7", version = None, deps = [],
          scope = "machine", container = "toolchain", container_arch = "target", **kwargs):
    mc = ctx.machine_config
    d = distro or ctx.default_distro_override or ctx.default_distro
    if not d:
        fail("image %s: no distro set and the project has no defaults.distro" % name)
    family = _family(d)

    for f in features:
        if f not in FEATURES:
            fail("image %s: unknown feature %r (valid: %s)" % (name, f, ", ".join(FEATURES)))
    features = list(features)
    if "verity" in features and "secureboot" not in features:
        fail("image %s: verity needs secureboot - the root hash only means something in a signed command line. Use features = [\"secureboot\", \"verity\"] with either bootloader = \"uki\" (the firmware db trusts osb's key) or bootloader = \"shim\" (the distro's shim verifies osb's UKI through MOK)." % name)
    if "encrypt" in features and "tpm" not in features:
        fail("image %s: encrypt needs tpm - the data key is sealed to the TPM" % name)
    if "verity" in features and "readonly" not in features:
        features.append("readonly")

    loader = bootloader or ("uki" if "secureboot" in features else mc.bootloader) or ("limine" if mc.firmware == "bios" else "grub")
    if loader not in LOADERS:
        fail("image %s: unknown bootloader %r (valid: %s)" % (name, loader, ", ".join(LOADERS)))
    if "secureboot" in features and loader not in ["uki", "shim"]:
        fail("image %s: secureboot signs an osb UKI - use bootloader = \"uki\" (or leave it unset), or bootloader = \"shim\" to have the distro's shim verify that UKI through MOK instead of putting osb's key in the firmware db" % name)
    if loader == "shim" and family != "apt":
        return _unsupported(name, d, "bootloader = \"shim\" uses the distro's signed shim, which debian and ubuntu ship and %s does not" % d)
    if loader == "shim" and "secureboot" in features and "ab" in features:
        # shim loads exactly one signed binary as its second stage, so there is no
        # boot counting behind it; A/B would stage a second slot nothing can boot.
        fail("image %s: ab with bootloader = \"shim\" has no rollback - shim loads a single signed UKI. Use bootloader = \"uki\" with secureboot for A/B, or drop ab." % name)
    if mc.firmware == "bios":
        if loader != "limine":
            return _unsupported(name, d, "bios firmware boots through limine only, not %s" % loader)
        for f in ["secureboot", "verity", "encrypt", "ab"]:
            if f in features:
                return _unsupported(name, d, "%s needs uefi firmware (machine %s is bios)" % (f, mc.name))

    parts = layout or default_layout(mc.firmware, features)
    check_layout(parts, mc.firmware, features)
    grow = len([p for p in parts if p["grow"]]) > 0

    pkgs = list(packages) + list(distro_packages.get(d, []))
    pkgs += list(mc.packages) + list(mc.distro_packages.get(d, []))
    pkgs += _feature_packages("initrd", family) + _feature_packages(loader, family)
    if loader == "shim" and "secureboot" in features:
        # shim verifies osb's UKI, so the UKI stub is needed as well; mokutil lets
        # the running system request a key for MOK (a rotation, or the live
        # installer enrolling before it writes the target disk).
        pkgs += _feature_packages("uki", family)
        pkgs.append("mokutil")
    for f in features:
        if f in _PACKAGES:
            pkgs += _feature_packages(f, family)
    if grow:
        pkgs += _feature_packages("grow", family)
    if iso:
        pkgs += _feature_packages("limine", family)
    if family == "apt":
        pkgs += _APT_ESSENTIAL
    if init:
        pkgs = select("init", init, pkgs)
    assert_single("init", pkgs, name)

    kernel = mc.kernel.get(d) or mc.kernel.get("")
    explicit = []
    for p in pkgs:
        if p == "linux":
            if not kernel:
                fail("image %s: machine %s has no kernel for distro %s" % (name, mc.name, d))
            p = kernel
        p = ctx.provides.get(p, p)
        if p not in explicit:
            explicit.append(p)
    resolved = resolve_closure(explicit, distro = d)

    entries = _boot_entries(parts, features, mc, cmdline, tpm_pcrs)
    users = users or [user(name = "root")]
    if type(services) == "dict":
        services = services.get(d, [])
    host = hostname or ctx.machine.replace("_", "-")
    table = "dos" if mc.firmware == "bios" else "gpt"
    initial = entries[0]["root"] if len(entries) > 1 else ""
    timeout = 1 if loader == "grub" else 3

    def rootfs():
        if family == "apt":
            _assemble_apt(resolved)
        else:
            _assemble_alpine(resolved)
        _configure(host, timezone, users, services, parts)

    def disk():
        stage_boot(loader, mc.firmware, entries, timeout, "secureboot" in features)
        assemble_disk(name, parts, table, "verity" in features, initial)
        if mc.firmware == "bios":
            install_bios_loader(name + ".img")
        if loader in ["uki", "shim"] and "secureboot" in features:
            install_uki(
                image = name + ".img",
                kernel = "uki/vmlinuz",
                initrd = "uki/initrd.img",
                stub = "uki/stub.efi",
                entries = entries,
                secureboot = True,
                shim = loader == "shim",
            )
        run("rm -rf $DESTDIR/bootfs $DESTDIR/uki $DESTDIR/layout.tsv", privileged = True)

    tasks = [task("rootfs", fn = rootfs), task("disk", fn = disk)]
    if iso:
        console = " ".join(["console=" + c for c in mc.console.split(" ") if c]) if mc.console else ""
        # The installer boots the image's own kernel, so it needs the machine's
        # command line as well as the console: a virtual machine with no serial
        # port shows nothing at all when the kernel is told console=ttyS0 only,
        # and the image's cmdline is how an unattended install passes
        # osb.target=/dev/vda.
        iso_args = " ".join([a for a in [console, mc.cmdline, cmdline] if a])
        tasks.append(task("iso", fn = lambda: make_iso(name, iso_args)))

    all_deps = list(deps)
    if container and container not in all_deps:
        all_deps.append(container)

    unit(
        name = name,
        version = version or ctx.project_version,
        scope = scope,
        unit_class = "image",
        distro = d,
        packages = resolved,
        boot = {
            "loader": loader,
            "firmware": mc.firmware,
            "features": features,
            "entries": entries,
        },
        layout = parts,
        users = users,
        services = services,
        hostname = host,
        timezone = timezone,
        iso = iso,
        container = container,
        container_arch = container_arch,
        sandbox = True,
        shell = "bash",
        deps = all_deps,
        tasks = tasks,
        **kwargs
    )

def _unsupported(name, distro, why):
    msg = "image %s cannot be built for machine %s: %s" % (name, ctx.machine, why)
    unit(name = name, unit_class = "image", distro = distro, scope = "machine",
         container = "toolchain", container_arch = "target", deps = ["toolchain"],
         tasks = [task("check", fn = lambda: fail(msg))])

def _ref(p, table_gpt):
    return ("PARTLABEL=" if table_gpt else "LABEL=") + p["name"]

def _boot_entries(parts, features, mc, extra, tpm_pcrs):
    gpt = mc.firmware != "bios"
    base = ["console=" + c for c in mc.console.split(" ") if c] if mc.console else []
    base += [a for a in [mc.cmdline, extra] if a]
    readonly = "readonly" in features
    common = []
    if readonly:
        common.append("osb.overlay=tmpfs")
    for p in parts:
        if p["encrypt"]:
            common.append("osb.crypt=" + _ref(p, gpt))
        if p["grow"]:
            common.append("osb.grow=" + _ref(p, gpt))
    if "tpm" in features:
        common.append("osb.tpm.pcrs=" + tpm_pcrs)
    roots = root_partitions(parts)
    entries = []
    for i, r in enumerate(roots):
        args = base + [
            "root=" + _ref(r, gpt),
            "rootfstype=" + r["fs"],
            "rw",
        ] + common
        if r["slot"]:
            args.append("osb.slot=" + r["slot"])
        h = verity_partition(parts, r["slot"])
        entries.append({
            "slot": r["slot"] or "a",
            "root": r["name"],
            "hash": h["name"] if h else "",
            "cmdline": " ".join(args),
            "initial": i == 0,
        })
    return entries

def _assemble_alpine(packages):
    run("mkdir -p $DESTDIR/rootfs/etc/apk/keys && cp $OSB_KEYS_DIR/$OSB_KEY_NAME $DESTDIR/rootfs/etc/apk/keys/")
    run("apk add --root $DESTDIR/rootfs --initdb --no-network --no-cache -X $REPO " + " ".join(packages),
        privileged = True)

def _assemble_apt(packages):
    run("""
set -eu
case "$ARCH" in
    x86_64) debarch=amd64 ;;
    arm64)  debarch=arm64 ;;
esac
mkdir -p $DESTDIR/rootfs
mmdebstrap --mode=root --variant=custom \
  --setup-hook='for d in bin sbin lib lib64; do mkdir -p "$1/usr/$d"; ln -sf "usr/$d" "$1/$d"; done' \
  --extract-hook='mkdir -p "$1/etc/alternatives"; ln -sf /usr/bin/mawk "$1/etc/alternatives/awk"; ln -sf /etc/alternatives/awk "$1/usr/bin/awk"' \
  --architectures="$debarch" --include="%s" \
  --aptopt='APT::Get::Install-Recommends "false"' --aptopt='Acquire::Check-Valid-Until "false"' \
  "$SUITE" "$DESTDIR/rootfs" "deb [trusted=yes] copy:$REPO $SUITE main"
broken=$(chroot $DESTDIR/rootfs dpkg-query -W -f='${db:Status-Abbrev} ${binary:Package}\\n' | grep -v '^ii ' || true)
if [ -n "$broken" ]; then
    echo "rootfs: packages not fully installed:" >&2
    echo "$broken" >&2
    exit 1
fi
: > $DESTDIR/rootfs/etc/apt/sources.list
""" % ",".join(packages), privileged = True)

def _configure(hostname, timezone, users, services, parts):
    lines = ["set -euo pipefail", "R=$DESTDIR/rootfs"]
    lines.append("echo %s > $R/etc/hostname" % hostname)
    if timezone:
        lines.append("ln -sf /usr/share/zoneinfo/%s $R/etc/localtime && echo %s > $R/etc/timezone" % (timezone, timezone))

    fstab = []
    for p in parts:
        if p["role"] == "swap":
            fstab.append("LABEL=%s none swap defaults 0 0" % p["name"])
        if not p["mount"] or p["mount"] == "/":
            continue
        lines.append("mkdir -p $R%s" % p["mount"])
        if p["encrypt"]:
            fstab.append("/dev/mapper/%s %s ext4 defaults,nofail 0 2" % (p["name"], p["mount"]))
        elif p["fs"] == "vfat":
            fstab.append("LABEL=%s %s vfat umask=0077,nofail 0 2" % (p["name"].upper()[:11], p["mount"]))
        else:
            fstab.append("LABEL=%s %s %s defaults,nofail 0 2" % (p["name"], p["mount"], p["fs"]))
    lines.append("cat > $R/etc/fstab <<'OSB_EOF'\n" + "\n".join(fstab) + "\nOSB_EOF")

    lines.append("""
hash_pw() {
    [ -z "$1" ] && return
    openssl passwd -6 -salt "$(printf '%s' "$2" | sha256sum | cut -c1-16)" "$1"
}
add_user() {
    name=$1 uid=$2 gid=$3 home=$4 shell=$5 pw=$6 groups=$7
    touch $R/etc/passwd $R/etc/group $R/etc/shadow
    sed -i "/^$name:/d" $R/etc/passwd $R/etc/shadow
    grep -q "^[^:]*:x:$gid:" $R/etc/group || echo "$name:x:$gid:" >> $R/etc/group
    echo "$name:x:$uid:$gid::$home:$shell" >> $R/etc/passwd
    echo "$name:$(hash_pw "$pw" "$name"):19000:0:99999:7:::" >> $R/etc/shadow
    mkdir -p "$R$home"
    chown "$uid:$gid" "$R$home"
    [ "$uid" = 0 ] && chmod 0700 "$R$home"
    for g in $groups; do
        if ! grep -q "^$g:" $R/etc/group; then
            echo "$g:x:$(( 900 + $(wc -l < $R/etc/group) )):" >> $R/etc/group
        fi
        members=$(grep "^$g:" $R/etc/group | cut -d: -f4)
        case ",$members," in *",$name,"*) continue ;; esac
        sed -i "s/^\\($g:[^:]*:[^:]*:\\).*/\\1${members:+$members,}$name/" $R/etc/group
    done
}
chmod 0640 $R/etc/shadow 2>/dev/null || true
""")
    root_open = False
    for u in users:
        shell = u["shell"]
        if u["uid"] == 0 and shell == "/bin/sh":
            shell = "$( [ -x $R/bin/bash ] && echo /bin/bash || echo /bin/sh )"
        lines.append("add_user %s %d %d %s \"%s\" '%s' '%s'" % (
            u["name"], u["uid"], u["gid"], u["home"], shell,
            u["password"].replace("'", "'\"'\"'"), " ".join(u["groups"])))
        if u["uid"] == 0 and not u["password"]:
            root_open = True
    if root_open:
        lines.append("""
if [ -d $R/etc/ssh ]; then
    mkdir -p $R/etc/ssh/sshd_config.d
    printf 'PermitRootLogin yes\\nPermitEmptyPasswords yes\\n' > $R/etc/ssh/sshd_config.d/10-osb-dev.conf
fi""")

    for s in services:
        lines.append("""
if [ -x $R/sbin/openrc ] || [ -d $R/etc/runlevels ]; then
    mkdir -p $R/etc/runlevels/default && ln -sf /etc/init.d/%s $R/etc/runlevels/default/%s
else
    chroot $R systemctl enable %s
fi""" % (s, s, s))

    lines.append("""
mount --bind /dev $R/dev
mount -t proc proc $R/proc
trap 'umount $R/proc; umount $R/dev' EXIT
for kv in $(ls $R/lib/modules 2>/dev/null); do chroot $R depmod -a "$kv"; done
chroot $R /bin/sh /usr/lib/osb/mkinitrd /boot/osb-initrd.img
""")
    run("\n".join(lines), privileged = True)
