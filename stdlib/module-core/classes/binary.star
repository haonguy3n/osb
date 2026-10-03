load("//classes/tasks.star", "merge_tasks")


_DEFAULT_ARCH_MAP = {
    "x86_64": "amd64",
    "arm64":  "arm64",
}

def _subst(s, version, arch_token):
    return s.replace("{version}", version).replace("{arch}", arch_token)

def _basename(path):
    if "/" not in path:
        return path
    return path.rsplit("/", 1)[1]

def _relpath(from_dir, to_path):
    fp = from_dir.split("/")
    tp = to_path.split("/")
    i = 0
    common = min(len(fp), len(tp))
    for j in range(common):
        if fp[j] != tp[j]:
            break
        i = j + 1
    ups = [".."] * (len(fp) - i)
    rest = tp[i:]
    if not ups and not rest:
        return "."
    return "/".join(ups + rest)

def _normalise_binaries(binaries, default_name, version, arch_token):
    if binaries == None:
        return [(default_name, _subst(default_name, version, arch_token))]
    if type(binaries) == "list":
        out = []
        for entry in binaries:
            if type(entry) != "string":
                fail("binary: 'binaries' list entries must be strings, got %r" % entry)
            src = _subst(entry, version, arch_token)
            out.append((_basename(entry), src))
        return out
    if type(binaries) == "dict":
        out = []
        for k, v in binaries.items():
            if type(k) != "string" or type(v) != "string":
                fail("binary: 'binaries' dict entries must be string→string")
            if "/" in k:
                fail("binary: 'binaries' install name %r cannot contain '/'" % k)
            out.append((k, _subst(v, version, arch_token)))
        return out
    fail("binary: 'binaries' must be list, dict, or omitted")

def _install_steps(name, binaries_pairs, install_tree, extras, symlinks):
    steps = []
    if install_tree:
        steps.append("mkdir -p $DESTDIR%s" % install_tree)
        steps.append("cp -aT . $DESTDIR%s" % install_tree)

    for install_name, src in binaries_pairs:
        dst_dir = "$DESTDIR$PREFIX/bin"
        dst = "%s/%s" % (dst_dir, install_name)
        if install_tree:
            target_abs = "%s/%s" % (install_tree, src)
            target_rel = _relpath("$PREFIX/bin", target_abs)
            steps.append("mkdir -p %s" % dst_dir)
            steps.append("ln -sfn %s %s" % (target_rel, dst))
        else:
            steps.append("mkdir -p %s" % dst_dir)
            steps.append("install -m0755 ./%s %s" % (src, dst))

    for entry in extras:
        if len(entry) == 2:
            src, dst = entry[0], entry[1]
            mode = None
        elif len(entry) == 3:
            src, dst, mode = entry[0], entry[1], entry[2]
        else:
            fail("binary: extras entries must be (src, dst) or (src, dst, mode)")
        steps.append("mkdir -p $(dirname $DESTDIR%s)" % dst)
        steps.append("cp -aT ./%s $DESTDIR%s" % (src, dst))
        if mode != None:
            steps.append("chmod %o $DESTDIR%s" % (mode, dst))

    for dst, target in symlinks.items():
        steps.append("mkdir -p $(dirname $DESTDIR%s)" % dst)
        steps.append("ln -sfn %s $DESTDIR%s" % (target, dst))

    if not steps:
        fail("binary %s: no install steps - set 'binaries' or 'extras'" % name)
    return steps

def binary(name, version, base_url, sha256,
           asset = None, assets = None, arch_map = None,
           binaries = None, install_tree = "",
           extras = [], symlinks = {},
           container = "toolchain",
           container_arch = "target",
           deps = [], runtime_deps = [],
           license = "", description = "",
           services = [], conffiles = [], scope = "",
           tasks = [], **kwargs):
    if ctx.arch not in sha256:
        fail("binary %s: sha256 has no entry for arch=%s" % (name, ctx.arch))

    if (asset == None) == (assets == None):
        fail("binary %s: set exactly one of 'asset' (template) or 'assets' (per-arch dict)" % name)

    amap = arch_map if arch_map != None else _DEFAULT_ARCH_MAP

    if assets != None:
        if ctx.arch not in assets:
            fail("binary %s: assets has no entry for arch=%s" % (name, ctx.arch))
        arch_token = ""
        asset_path = _subst(assets[ctx.arch], version, arch_token)
    else:
        if ctx.arch not in amap:
            fail("binary %s: arch_map has no entry for arch=%s" % (name, ctx.arch))
        arch_token = amap[ctx.arch]
        asset_path = _subst(asset, version, arch_token)

    src_arch_token = arch_token
    if src_arch_token == "":
        src_arch_token = amap[ctx.arch] if ctx.arch in amap else ctx.arch

    binaries_pairs = _normalise_binaries(binaries, name, version, src_arch_token)
    sha = sha256[ctx.arch]

    final_base_url = _subst(base_url, version, src_arch_token)
    source_url = final_base_url + "/" + asset_path

    final_install_tree = _subst(install_tree, version, src_arch_token) if install_tree else ""
    final_extras = []
    for e in extras:
        if len(e) == 2:
            final_extras.append((_subst(e[0], version, src_arch_token), e[1]))
        else:
            final_extras.append((_subst(e[0], version, src_arch_token), e[1], e[2]))
    final_symlinks = {}
    for k, v in symlinks.items():
        final_symlinks[k] = _subst(v, version, src_arch_token)

    install_task = task("install", steps = _install_steps(
        name, binaries_pairs, final_install_tree, final_extras, final_symlinks,
    ))
    final_tasks = merge_tasks([install_task], tasks)

    all_deps = list(deps)
    if container and ":" not in container and container not in all_deps:
        all_deps.append(container)

    unit(
        name = name,
        version = version,
        source = source_url,
        sha256 = sha,
        deps = all_deps,
        runtime_deps = runtime_deps,
        tasks = final_tasks,
        services = services,
        conffiles = conffiles,
        license = license,
        description = description,
        scope = scope,
        container = container,
        container_arch = container_arch,
        sandbox = False,
        **kwargs
    )
