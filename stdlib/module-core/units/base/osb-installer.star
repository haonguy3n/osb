# osb-installer - the on-device guided installer.
#
# Built from osb's own Go module rather than a vendored copy: the installer
# shares internal/installer with the host binary, so the planning logic that
# `go test ./internal/installer` covers is exactly the logic that runs on the
# target. Pointing this at a git tag instead would let the two drift.
#
# SOURCE PIN. This tracks `main` because the repository has no tagged releases
# yet, and pinning to a tag that does not exist fails the fetch outright. A
# moving branch is at odds with osb's reproducibility guarantees - the unit's
# input hash will not change when upstream main does - so this must become
# `tag = "vX.Y.Z"` as soon as the first release is cut. Projects that need a
# fixed build today should override this unit in their own units/ directory
# (the stdlib is injected at the lowest priority, so a same-named unit wins)
# and pin a tag or a fork.
#
# Container: golang on the *host* arch cross-compiling to $ARCH, the same
# arrangement go_binary uses. CGO is off, so the result is a static binary that
# runs on musl and glibc alike - the installer must work in whatever live image
# it is dropped into.
load("//classes/go.star", "go_binary")

go_binary(
    name = "osb-installer",
    version = "0.1.0",
    source = "https://github.com/anhhao17/osb.git",
    branch = "main",
    go_package = "./cmd/osb-installer",
    license = "Apache-2.0",
    description = "Guided on-device installer (partition, LUKS2, users, bootloader)",
    # Every external command Plan() emits has to exist in the live image, or
    # the install dies partway through with "executable file not found" after
    # the disk has already been repartitioned. Keep this list in step with
    # internal/installer/plan.go and configure.go:
    #   sfdisk, wipefs  -> util-linux
    #   udevadm         -> eudev
    #   mkfs.ext4       -> e2fsprogs
    #   mkfs.vfat       -> dosfstools
    #   cryptsetup      -> cryptsetup
    #   rsync           -> rsync
    #   limine          -> limine
    #   chroot, mount, umount, mkdir, cp, sync, chpasswd, adduser, addgroup
    #                   -> busybox / util-linux
    runtime_deps = [
        "util-linux",
        "e2fsprogs",
        "dosfstools",
        "cryptsetup",
        "rsync",
        "eudev",
        "limine",
    ],
)
