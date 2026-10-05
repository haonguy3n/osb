load("//classes/tasks.star", "merge_tasks")


def python_venv(name, version, pip_packages,
                install_path = "",
                entry_points = {},
                deps = [], runtime_deps = [],
                services = [], conffiles = [],
                license = "", description = "",
                tasks = [], scope = "",
                container = "toolchain", container_arch = "target",
                **kwargs):
    if not pip_packages:
        fail("python_venv %s: pip_packages must not be empty" % name)

    if not install_path:
        install_path = "/usr/lib/python-venvs/" + name

    parent = _parent_dir(install_path)
    pkg_args = " ".join(["'" + p + "'" for p in pip_packages])

    wrapper_block = ""
    if entry_points:
        wrapper_block = "mkdir -p $DESTDIR/usr/bin\n"
        for bin_name, entry in entry_points.items():
            wrapper_block += _entry_point_script(install_path, bin_name, entry)

    venv_script = """
set -e
VENV_INSTALL=%s
VENV_BUILD=$DESTDIR$VENV_INSTALL
mkdir -p $DESTDIR%s
python3 -m venv "$VENV_BUILD"
"$VENV_BUILD/bin/pip" install --no-cache-dir --disable-pip-version-check --upgrade pip
"$VENV_BUILD/bin/pip" install --no-cache-dir --disable-pip-version-check %s
find "$VENV_BUILD" -type d -name __pycache__ -prune -exec rm -rf {} +
grep -rIlF "$VENV_BUILD" "$VENV_BUILD" | xargs -r sed -i "s|$VENV_BUILD|$VENV_INSTALL|g"
ln -sfn /usr/bin/python3 "$VENV_BUILD/bin/python"
ln -sfn python "$VENV_BUILD/bin/python3"
%s""" % (install_path, parent, pkg_args, wrapper_block)

    base_tasks = [task("build", steps = [venv_script])]
    final_tasks = merge_tasks(base_tasks, tasks)

    all_runtime_deps = list(runtime_deps)
    if "python3" not in all_runtime_deps:
        all_runtime_deps.append("python3")

    all_deps = list(deps)
    if container and ":" not in container and container not in all_deps:
        all_deps.append(container)
    if "python3" not in all_deps:
        all_deps.append("python3")
    if "py3-pip" not in all_deps:
        all_deps.append("py3-pip")

    unit(
        name = name,
        version = version,
        deps = all_deps,
        runtime_deps = all_runtime_deps,
        tasks = final_tasks,
        services = services,
        conffiles = conffiles,
        license = license,
        description = description,
        scope = scope,
        container = container,
        container_arch = container_arch,
        sandbox = False,
        shell = "bash",
        **kwargs
    )

def _entry_point_script(install_path, bin_name, entry):
    if ":" in entry:
        module, func = entry.split(":", 1)
        body = "exec %s/bin/python -c \"import sys; from %s import %s; sys.exit(%s(*sys.argv[1:]))\" \"$@\"" % (
            install_path, module, func, func,
        )
    else:
        body = "exec %s/bin/python -m %s \"$@\"" % (install_path, entry)
    dest = "$DESTDIR/usr/bin/" + bin_name
    return (
        "cat > %s <<'__OSB_PY_WRAP_EOF__'\n" % dest +
        "#!/bin/sh\n" +
        "%s\n" % body +
        "__OSB_PY_WRAP_EOF__\n" +
        "chmod 0755 %s\n" % dest
    )

def _parent_dir(path):
    if "/" not in path:
        return "."
    return path.rsplit("/", 1)[0]
