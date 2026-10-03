_FAMILIES = {
    "init": {
        "openrc": ["openrc"],
        "systemd": ["systemd-sysv", "systemd-resolved"],
        "sysvinit": ["sysvinit-core"],
        "busybox": [],
    },
}

_DEFAULTS = {
    "init": {
        "alpine": "openrc",
        "debian": "systemd",
        "ubuntu": "systemd",
    },
}

def choices(slot):
    return sorted(_FAMILIES[slot].keys())

def default_choice(slot, distro):
    return _DEFAULTS.get(slot, {}).get(distro, "")

def packages_for(slot, choice):
    if slot not in _FAMILIES:
        fail("unknown exclusive slot %r" % slot)
    fam = _FAMILIES[slot]
    if choice not in fam:
        fail("unknown %s %r (valid: %s)" % (slot, choice, ", ".join(choices(slot))))
    return list(fam[choice])

def detect(slot, artifacts):
    present = {}
    for choice, pkgs in _FAMILIES[slot].items():
        for p in pkgs:
            if p in artifacts:
                present[choice] = True
    return sorted(present.keys())

def select(slot, choice, artifacts):
    drop = {}
    for other, pkgs in _FAMILIES[slot].items():
        if other != choice:
            for p in pkgs:
                drop[p] = True
    kept = [a for a in artifacts if a not in drop]
    for p in packages_for(slot, choice):
        if p not in kept:
            kept.append(p)
    return kept

def assert_single(slot, artifacts, image_name):
    found = detect(slot, artifacts)
    if len(found) > 1:
        fail(
            ("image %s selects %d %s systems (%s) but may have only one - " +
             "pass %s = \"<one>\" to image(), or drop the others from its " +
             "artifacts. Valid: %s") %
            (image_name, len(found), slot, ", ".join(found), slot,
             ", ".join(choices(slot))),
        )
