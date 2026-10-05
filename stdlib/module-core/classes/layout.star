_FS = ["ext4", "vfat", "verity", "swap", "raw"]
_ROLES = ["esp", "boot", "root", "verity", "data", "swap", "raw"]

def part(name, size = "auto", fs = "ext4", mount = None, role = None, slot = "",
         grow = False, encrypt = False, source = None, type = None, offset = None):
    if fs not in _FS:
        fail("part %s: fs must be one of %s, got %r" % (name, _FS, fs))
    if role == None:
        role = _infer_role(name, fs, mount)
    if role not in _ROLES:
        fail("part %s: role must be one of %s, got %r" % (name, _ROLES, role))
    if slot not in ["", "a", "b"]:
        fail("part %s: slot must be \"a\", \"b\" or empty" % name)
    if encrypt and fs != "ext4":
        fail("part %s: only ext4 partitions can be encrypted" % name)
    if role == "esp" and mount == None:
        mount = "/boot/efi"
    return {
        "name": name,
        "size": str(size),
        "fs": fs,
        "mount": mount or "",
        "role": role,
        "slot": slot,
        "grow": grow,
        "encrypt": encrypt,
        "source": source or "",
        "type": type or "",
        "offset": str(offset) if offset else "",
    }

def _infer_role(name, fs, mount):
    if mount == "/":
        return "root"
    if fs == "verity":
        return "verity"
    if fs == "swap":
        return "swap"
    if fs == "raw":
        return "raw"
    if fs == "vfat" and name in ["esp", "efi"]:
        return "esp"
    if fs == "vfat" and name == "boot":
        return "boot"
    return "data"

def default_layout(firmware, features):
    parts = []
    if firmware == "bios":
        parts.append(part("boot", fs = "vfat", size = "256M", role = "boot", mount = "/boot/efi"))
    else:
        parts.append(part("esp", fs = "vfat", size = "256M"))
    slots = ["a", "b"] if "ab" in features else [""]
    for s in slots:
        root = "root-" + s if s else "root"
        parts.append(part(root, mount = "/", slot = s))
        if "verity" in features:
            parts.append(part(root + "-hash", fs = "verity", slot = s))
    if "ab" in features or "encrypt" in features or "readonly" in features or "verity" in features:
        parts.append(part("data", size = "256M", mount = "/data", grow = True, encrypt = "encrypt" in features))
    else:
        parts[-1]["grow"] = True
    return parts

def check_layout(parts, firmware, features):
    names = {}
    for p in parts:
        if p["name"] in names:
            fail("layout: partition name %r is used twice" % p["name"])
        names[p["name"]] = True
        if len(p["name"]) > 16:
            fail("layout: partition name %r is longer than 16 characters" % p["name"])
    roots = [p for p in parts if p["role"] == "root"]
    if not roots:
        fail("layout: needs a partition with mount = \"/\"")
    slots = [p["slot"] for p in roots]
    if len(roots) > 1 and sorted(slots) != ["a", "b"]:
        fail("layout: two root partitions must be slot \"a\" and slot \"b\"")
    if ("ab" in features) != (len(roots) == 2):
        fail("layout: the ab feature needs exactly one root per slot a and b")
    if firmware == "uefi" and not [p for p in parts if p["role"] == "esp"]:
        fail("layout: uefi firmware needs an esp partition")
    if firmware == "bios" and not [p for p in parts if p["role"] == "boot" and p["fs"] == "vfat"]:
        fail("layout: bios firmware needs a vfat partition with role \"boot\"")
    for r in roots:
        hashes = [p for p in parts if p["role"] == "verity" and p["slot"] == r["slot"]]
        if "verity" in features and len(hashes) != 1:
            fail("layout: verity needs one fs = \"verity\" partition for root %r" % r["name"])
    if "encrypt" in features and not [p for p in parts if p["encrypt"]]:
        fail("layout: the encrypt feature needs a partition with encrypt = True")
    if [p for p in parts if p["encrypt"]] and "encrypt" not in features:
        fail("layout: encrypted partitions need the encrypt feature")
    if len([p for p in parts if p["grow"]]) > 1:
        fail("layout: only one partition can grow")
    for p in parts:
        if p["grow"] and p != parts[-1]:
            fail("layout: only the last partition can grow (%r is not last)" % p["name"])

def boot_partition(parts):
    for p in parts:
        if p["role"] in ["esp", "boot"]:
            return p
    fail("layout: no esp or boot partition")

def root_partitions(parts):
    return [p for p in parts if p["role"] == "root"]

def verity_partition(parts, slot):
    for p in parts:
        if p["role"] == "verity" and p["slot"] == slot:
            return p
    return None
