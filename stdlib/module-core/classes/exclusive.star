# Exclusive options: a family of mutually exclusive choices where an image
# must resolve to exactly one, in the spirit of Yocto's virtual/ providers and
# PREFERRED_PROVIDER.
#
# Two packages that both claim PID 1 do not fail the build - they produce an
# image that boots the wrong one, or not at all. The check below turns that
# into a build error naming both, which is the whole point of the mechanism.

# _FAMILIES maps an exclusive slot to {choice: packages that implement it}.
# A package listed here is a marker: seeing it in a closure means that choice
# is present. Add a new implementation by adding an entry, not by editing the
# image class.
_FAMILIES = {
    "init": {
        "openrc": ["openrc"],
        "systemd": ["systemd-sysv", "systemd-resolved"],
        "sysvinit": ["sysvinit-core"],
        # busybox's built-in init needs no package of its own; selecting it
        # simply means adding none of the above.
        "busybox": [],
    },
}

# Conventional choice per distro when an image does not name one, matching
# what each distro's own base system ships.
_DEFAULTS = {
    "init": {
        "alpine": "openrc",
        "debian": "systemd",
        "ubuntu": "systemd",
    },
}

def choices(slot):
    """Return the valid choices for an exclusive slot, sorted."""
    return sorted(_FAMILIES[slot].keys())

def default_choice(slot, distro):
    """Return the conventional choice for a distro, or "" if it has none."""
    return _DEFAULTS.get(slot, {}).get(distro, "")

def packages_for(slot, choice):
    """Return the packages implementing one choice.

    Fails on an unknown choice rather than silently installing nothing, which
    would otherwise surface as an image with no PID 1.
    """
    if slot not in _FAMILIES:
        fail("unknown exclusive slot %r" % slot)
    fam = _FAMILIES[slot]
    if choice not in fam:
        fail("unknown %s %r (valid: %s)" % (slot, choice, ", ".join(choices(slot))))
    return list(fam[choice])

def detect(slot, artifacts):
    """Return the choices an artifact list pulls in, sorted.

    Marker-based, so it sees a choice however it arrived - named directly by
    the image, inherited from a baseline set, or added by a machine.
    """
    present = {}
    for choice, pkgs in _FAMILIES[slot].items():
        for p in pkgs:
            if p in artifacts:
                present[choice] = True
    return sorted(present.keys())

def select(slot, choice, artifacts):
    """Return artifacts with `choice` as the only implementation of slot.

    Strips every other choice's packages before adding this one, so naming a
    choice overrides whatever a baseline set brought in instead of colliding
    with it - an Alpine image can select systemd without also having to edit
    ALPINE_BASE.
    """
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
    """Fail unless the artifacts resolve to at most one choice for slot."""
    found = detect(slot, artifacts)
    if len(found) > 1:
        fail(
            ("image %s selects %d %s systems (%s) but may have only one - " +
             "pass %s = \"<one>\" to image(), or drop the others from its " +
             "artifacts. Valid: %s") %
            (image_name, len(found), slot, ", ".join(found), slot,
             ", ".join(choices(slot))),
        )
