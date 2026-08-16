# osb-installer - the on-device guided installer.
#
# Built from osb's own Go module, not a vendored copy, so the planning logic
# `go test ./internal/installer` covers is exactly what runs on the target.
#
# SOURCE PIN: tracks `main` because there are no tagged releases yet. A moving
# branch is at odds with reproducibility - the unit's input hash does not
# change when upstream does - so make this `tag = "vX.Y.Z"` once a release
# exists. To pin today, override this unit in your own units/ directory.
#
# CGO is off, so the binary is static and runs on musl and glibc alike.
load("//classes/go.star", "go_binary")

go_binary(
    name = "osb-installer",
    version = "0.1.0",
    source = "https://github.com/anhhao17/osb.git",
    branch = "main",
    go_package = "./cmd/osb-installer",
    license = "Apache-2.0",
    description = "Guided on-device installer (partition, LUKS2, users, bootloader)",
    # Every command Plan() emits must exist in the live image, or the install
    # dies partway through - after the disk is already repartitioned. Keep in
    # step with internal/installer/{plan,configure}.go. busybox covers chroot,
    # mount, umount, mkdir, cp, sync, chpasswd, adduser and addgroup.
    #
    # sfdisk and wipefs are separate Alpine packages, NOT part of `util-linux`
    # (that apk is PAM modules only) - depending on it left both out and the
    # install failed at step 1/20.
    runtime_deps = [
        "sfdisk",
        "wipefs",
        "e2fsprogs",
        "dosfstools",
        "cryptsetup",
        "rsync",
        "eudev",
        "limine",
        "mkinitfs",
    ],
)
