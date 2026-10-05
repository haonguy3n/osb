_CONTEXT_LABEL = "osb.context-hash"

def container(name, version, dockerfile="Dockerfile", scope="arch", **kwargs):
    unit(
        name=name,
        version=version,
        unit_class="container",
        scope=scope,
        tasks=[
            task("build", fn=lambda: _build_container(name, version, dockerfile)),
        ],
        **kwargs,
    )

# A container image depends only on its build context, and building one is the
# most expensive thing a from-scratch build does. Stamp the image with a hash of
# that context and reuse it when the stamp still matches, so an image restored by
# CI (docker load) or left over from an earlier build is not rebuilt from
# scratch. The unit hash already covers the same files within one build tree;
# this covers a *new* tree with the same context, which is what CI has.
# An upstream base image that moved under a fixed FROM is not detected - rebuild
# with `osb build -force` or drop the tag when that matters.
def _context_hash(name):
    return run(
        "find %s -type f -print0 | sort -z | xargs -0 -r sha256sum | sha256sum | cut -d' ' -f1" % name,
        host=True).stdout.strip()

def _image_context_hash(tag):
    cmd = ("docker image inspect -f '{{.Config.Labels}}' %s 2>/dev/null"
           + " | grep -o '%s:[0-9a-f]*' | cut -d: -f2 || true") % (tag, _CONTEXT_LABEL)
    return run(cmd, host=True).stdout.strip()

def _build_container(name, version, dockerfile):
    arch = ctx.arch
    tag = "osb/%s:%s-%s" % (name, version, arch)
    host_arch = run("uname -m", host=True).stdout.strip()
    if host_arch == "aarch64":
        host_arch = "arm64"

    want = _context_hash(name)
    label = ""
    if want:
        if _image_context_hash(tag) == want:
            run("echo '  %s: reusing the image built from this context'" % tag, host=True)
            return
        label = " --label %s=%s" % (_CONTEXT_LABEL, want)

    if arch != host_arch:
        run("docker buildx build --platform linux/%s --load -t %s%s -f %s/%s %s" % (
            arch, tag, label, name, dockerfile, name), host=True)
    else:
        run("docker build -t %s%s -f %s/%s %s" % (
            tag, label, name, dockerfile, name), host=True)
