# Static libraries, headers, crt0 objects and linker scripts for building
# EFI applications. Build-time only: nothing here ships shared objects, so
# consumers (efitools, EFI stubs) list it in deps but not runtime_deps.
unit(
    name = "gnu-efi",
    version = "4.0.4",
    source = "https://github.com/ncroxon/gnu-efi.git",
    tag = "4.0.4",
    license = "BSD-2-Clause",
    description = "development libraries and headers for building EFI applications",
    deps = ["toolchain"],
    container = "toolchain",
    container_arch = "target",
    tasks = [
        task("build", steps=[
            # gnu-efi passes LDFLAGS directly to ld, and its Makefiles are
            # not parallel-safe (Alpine builds with -j1 for the same reason).
            "unset LDFLAGS; make -j1",
            "unset LDFLAGS; make PREFIX=/usr INSTALLROOT=$DESTDIR install",
        ]),
    ],
)
