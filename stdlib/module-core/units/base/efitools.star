# Userspace Secure Boot key management: read/update EFI signature
# variables (PK/KEK/db/dbx) from Linux and convert certificates to signed
# EFI signature lists. Upstream also builds EFI applications (KeyTool.efi,
# LockDown.efi) but those need perl File::Slurp, help2man and sbsign at
# build time and osb already enrolls keys at image-build time, so only the
# userspace tools are built here.
_TOOLS = [
    "cert-to-efi-sig-list",
    "cert-to-efi-hash-list",
    "sig-list-to-certs",
    "sign-efi-sig-list",
    "hash-to-efi-sig-list",
    "efi-readvar",
    "efi-updatevar",
    "flash-var",
]

unit(
    name = "efitools",
    version = "1.9.2",
    source = "https://git.kernel.org/pub/scm/linux/kernel/git/jejb/efitools.git",
    tag = "v1.9.2",
    license = "(GPL-2.0-only AND LGPL-2.1-or-later) WITH OpenSSL-Exception",
    description = "tools for manipulating UEFI Secure Boot keys and signature databases",
    deps = ["gnu-efi", "openssl", "toolchain"],
    runtime_deps = ["openssl"],
    # Alpine's musl fixes: EFI variable names are CHAR16 but the tools used
    # wchar_t (32-bit on musl); console.c uses a gnu-efi constant whose typo
    # spelling was removed in gnu-efi 4.x.
    patches = [
        "efitools/003-fix-wchar_t.patch",
        "efitools/004-typo.patch",
    ],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps=[
            # Make.rules assigns INCDIR/CFLAGS outright (build env is
            # ignored) and hardcodes /usr/include/efi; the tools' link rules
            # have no -L. Point all three at the deps sysroot.
            "sed -i 's|-I/usr/include/efi|-I/build/sysroot/usr/include/efi|g' Make.rules",
            "sed -i 's|-O2 -g|-O2 -g -I/build/sysroot/usr/include -Wno-pointer-sign|' Make.rules",
            "sed -i 's|lib/lib.a -lcrypto|lib/lib.a -L/build/sysroot/usr/lib -lcrypto|' Makefile",
            # Explicit targets skip the EFI apps, man pages and demo keys
            # that `make all` would build. Each tool depends on lib/lib.a
            # via a recursive FORCE rule, so parallel make races it.
            "make -j1 " + " ".join(_TOOLS),
            "install -d $DESTDIR/usr/bin",
            "install -m 755 " + " ".join(_TOOLS) + " $DESTDIR/usr/bin/",
        ]),
    ],
)
