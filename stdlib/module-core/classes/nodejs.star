load("//classes/tasks.star", "merge_tasks")


def nodejs_app(name, version,
               install_path = "",
               entry_points = {},
               deps = [], runtime_deps = [],
               services = [], conffiles = [],
               license = "", description = "",
               tasks = [], scope = "",
               container = "toolchain", container_arch = "target",
               **kwargs):
    if not install_path:
        install_path = "/usr/lib/node-apps/" + name

    parent = _parent_dir(install_path)

    wrapper_block = ""
    if entry_points:
        wrapper_block = "mkdir -p $DESTDIR/usr/bin\n"
        for bin_name, entry in entry_points.items():
            wrapper_block += _entry_point_script(install_path, bin_name, entry)

    setup_script = """
set -e
APP_INSTALL=%s
APP_BUILD=$DESTDIR$APP_INSTALL
mkdir -p $DESTDIR%s
mkdir -p "$APP_BUILD"
""" % (install_path, parent)

    install_script = """
set -e
APP_INSTALL=%s
APP_BUILD=$DESTDIR$APP_INSTALL
test -f "$APP_BUILD/package.json" || {
    echo "nodejs_app %s: expected package.json at $APP_BUILD/package.json" >&2
    echo "  (use install_file(\\"package.json\\", \\"$APP_BUILD/package.json\\") in your task)" >&2
    exit 1
}
cd "$APP_BUILD"
if [ -f package-lock.json ]; then
    npm ci --omit=dev --no-audit --no-fund --loglevel=error
else
    npm install --omit=dev --no-audit --no-fund --loglevel=error
fi
if [ -d node_modules ]; then
    grep -rIlF "$APP_BUILD" node_modules 2>/dev/null \\
        | xargs -r sed -i "s|$APP_BUILD|$APP_INSTALL|g" || true
fi
%s""" % (install_path, name, wrapper_block)

    setup_task = task("nodejs-setup", steps = [setup_script])
    install_task = task("nodejs-install", steps = [install_script])

    final_tasks = merge_tasks([setup_task] + list(tasks) + [install_task], [])

    all_runtime_deps = list(runtime_deps)
    if "nodejs" not in all_runtime_deps:
        all_runtime_deps.append("nodejs")

    all_deps = list(deps)
    if container and ":" not in container and container not in all_deps:
        all_deps.append(container)
    if "nodejs" not in all_deps:
        all_deps.append("nodejs")
    if "npm" not in all_deps:
        all_deps.append("npm")

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
        pkg, script = entry.split(":", 1)
        body = "exec node %s/node_modules/%s/%s \"$@\"" % (
            install_path, pkg, script,
        )
    else:
        body = "exec %s/node_modules/.bin/%s \"$@\"" % (install_path, entry)
    dest = "$DESTDIR/usr/bin/" + bin_name
    return (
        "cat > %s <<'__OSB_NODE_WRAP_EOF__'\n" % dest +
        "#!/bin/sh\n" +
        "%s\n" % body +
        "__OSB_NODE_WRAP_EOF__\n" +
        "chmod 0755 %s\n" % dest
    )

def _parent_dir(path):
    if "/" not in path:
        return "."
    return path.rsplit("/", 1)[0]
