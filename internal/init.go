package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func RunInit(projectDir string, machine string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "PROJECT.star")); err == nil {
		return fmt.Errorf("project already exists at %s (PROJECT.star found)", projectDir)
	}

	dirs := []string{"machines", "units", "classes", "overlays"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	name := filepath.Base(projectDir)
	defaultMachine := machine
	if defaultMachine == "" {
		defaultMachine = "qemu-x86_64"
	}

	projectContent := fmt.Sprintf(`project(
    name = %q,
    version = "0.1.0",
    # defaults.distro selects the effective distro for any image that
    # doesn't set its own `+"`distro`"+` field. The cascade is
    #   image.distro -> local.default_distro_override -> defaults.distro
    # If all three are empty the closure walk errors at evaluation
    # time, so every project must declare at least one. Today all
    # images here are alpine-based, hence "alpine".
    defaults = defaults(
        machine = %q,
        image = "base-image",
        distro = "alpine",
    ),
    # osb bundles its standard library (core recipes, machines, images, and the
    # alpine/debian/ubuntu feeds) into the binary and injects it automatically
    # at the lowest priority, so this list is empty and the project needs no
    # external repositories. To customize, add your own module here — it is
    # evaluated after the bundled ones and therefore shadows them — or drop a
    # unit/machine/image under this project's units/, machines/, or classes/.
    modules = [],
    # Per-unit pins that override the default last-module-wins
    # shadowing, scoped per distro. The outer key is the consuming
    # image's effective distro, so an "alpine" pin has no effect on a
    # debian closure walk and vice versa — mixed-distro projects don't
    # need to drop pins to keep one backend resolving.
    prefer_modules = {
        "alpine": {
            # xz is built static-only in module-core, but kmod's depmod
            # needs the shared liblzma.so.5 — Alpine's prebuilt ships
            # it.
            "xz": "alpine.main",
            # module-core's source-built zstd ships libzstd.so.1 at
            # its own soversion. Alpine's nodejs links against
            # libzstd.so.1 from Alpine's zstd-libs, so mixing the two
            # trips an apk conflict (both packages own the same .so
            # path with incompatible versions). Pin zstd to Alpine so
            # the .so and CLI come from one source.
            "zstd": "alpine.main",
            # module-core's source-built util-linux is one monolithic
            # apk that bundles libblkid.so.1, libmount.so.1, and
            # libuuid.so.1 (via --enable-libblkid/--enable-libmount).
            # Alpine splits those libs into separate
            # libblkid/libmount/libuuid packages, which get pulled in
            # transitively by eudev, glib, e2fsprogs, etc. as soon as
            # an image grows past the base set. Both then claim
            # ownership of the same SONAMEs and apk refuses to
            # install. Pin util-linux to Alpine so util-linux and its
            # split libs come from one coordinated source.
            "util-linux": "alpine.main",
            # module-core's source-built curl bundles its own
            # libcurl.so.4 at 8.11.1's soversion. Alpine ships libcurl
            # as a separate package at 8.14.1 and other Alpine
            # packages (git, libcurl consumers) link against it.
            # Mixing both trips a so:libcurl.so.4 conflict the moment
            # an image pulls in git or any other libcurl consumer from
            # Alpine. Pin curl to Alpine so curl and libcurl come from
            # one coordinated source.
            "curl": "alpine.main",
            # module-core builds kmod 34.2 from source as one apk that
            # owns libkmod.so.2. The UEFI grub-efi package pulls Alpine's
            # mkinitfs, which depends on Alpine's split kmod-libs (33) —
            # a second owner of libkmod.so.2 at an incompatible version,
            # so apk refuses to install. Pin kmod to Alpine so kmod and
            # kmod-libs come from one coordinated source (same as the
            # debian/ubuntu kmod pins below).
            "kmod": "alpine.main",
        },
        # Most module-core source units consumed by a Debian image build
        # in the glibc container and package as .deb automatically (the
        # build-twice model), so no per-unit pin is needed just to pick
        # the Debian package format. Pins below are for the cases where a
        # monolithic module-core unit collides with Debian's split
        # packaging — same as the Alpine pins above, the lib and its
        # consumers must come from one coordinated source.
        "debian": {
            # module-core's source-built util-linux is a minimal
            # busybox-replacement build (--disable-all-programs) that
            # omits getopt, which Debian maintainer scripts
            # (update-initramfs) require, and it collides with Debian's
            # split util-linux/libuuid1/libmount1 family. Pin to Debian
            # so util-linux and its split libs come from one source.
            "util-linux": "debian.main",
            # module-core's source-built zstd is one package bundling
            # libzstd.so.1 and the CLI. Debian ships libzstd1 as a
            # separate package pulled transitively by libsystemd0,
            # libapt-pkg, etc.; both then own
            # /usr/lib/<tuple>/libzstd.so.1 and dpkg refuses to unpack.
            # Pin to Debian so the lib and CLI come from one source.
            "zstd": "debian.main",
            # module-core's source-built kmod bundles libkmod.so.2 with
            # the kmod tools. Debian splits libkmod2 from kmod and pulls
            # libkmod2 transitively (systemd, udev); both own
            # /usr/lib/<tuple>/libkmod.so.2. Pin to Debian so libkmod2
            # and the kmod tools come from one source.
            "kmod": "debian.main",
        },
        # Ubuntu derives from Debian and splits the same library families,
        # so the same module-core collisions apply — pin the same units to
        # ubuntu.main so each lib and its consumers come from one source.
        "ubuntu": {
            "util-linux": "ubuntu.main",
            "zstd": "ubuntu.main",
            "kmod": "ubuntu.main",
        },
    },
)
`, name, defaultMachine)

	if err := os.WriteFile(filepath.Join(projectDir, "PROJECT.star"), []byte(projectContent), 0644); err != nil {
		return fmt.Errorf("writing PROJECT.star: %w", err)
	}

	// Create .gitignore covering everything osb generates in a project tree:
	// build output, the module/source cache, the local apk repository, and the
	// per-developer local.star overrides. .claude/skills is intentionally not
	// ignored — those are project skills meant to be committed — but Claude
	// Code's per-user settings.local.json is.
	gitignore := `# Build output, caches, and the local apk repository
/build
/cache
/repo

# Per-developer settings written by osb (machine, image, parallel builds)
local.star

# Claude Code per-user local settings (skills under .claude/ are committed)
.claude/settings.local.json
`
	if err := os.WriteFile(filepath.Join(projectDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	fmt.Printf("Created Osb project at %s\n", projectDir)

	return nil
}
