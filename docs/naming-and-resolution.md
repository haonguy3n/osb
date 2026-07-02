# Naming and resolution

How a package name written in a unit's `deps`, an image's artifact list, or
a `prefer_modules` pin becomes exactly one concrete unit. This is the
reference for the resolution model; `osb desc <unit>` prints what the model
decided for any given name.

## Modules and priority

Units live in modules. Three kinds exist, in ascending priority:

1. **Synthetic feed modules** (`alpine.main`, `debian.main`, `ubuntu.main`,
   …) registered by `alpine_feed()` / `apt_feed()` calls in a distro
   module's MODULE.star. They always rank below every real module.
2. **Real modules**: the bundled stdlib (module-core and the distro
   modules), injected at the lowest real-module priority, then any modules
   listed in PROJECT.star's `modules = [...]`, in declaration order —
   later entries shadow earlier ones.
3. **The project itself**: `.star` files under the project's `units/`,
   `images/`, `machines/`, `classes/` shadow everything.

When two modules register the same unit name, the highest-priority one
wins ("last module wins"). Run osb with `--show-shadows` to see these
decisions as they happen.

## Distro visibility (R21a)

A non-image unit may carry a `distro` compatibility tag. The closure
walker filters out units whose tag doesn't match the consuming image's
effective distro; an untagged unit is visible to every distro (the common
case). This is how `alpine.main`'s packages never leak into a debian
closure even though both register the same names. The effective-distro
cascade for an image is:

```
image.distro -> local.star default_distro_override -> defaults.distro -> error
```

## prefer_modules: per-unit routing pins

When a name exists both as a source-built unit and in a feed (or in two
modules generally), module priority picks the source unit. A
`prefer_modules` pin overrides that per (distro, unit):

```python
prefer_modules = {"alpine": {"xz": "alpine.main"}}
```

Pins are keyed by the consuming image's effective distro, so an alpine pin
never disturbs a debian closure. They come from two places:

- **Module defaults**: a module's `module_info(prefer_modules = ...)`
  declares the pins its own packaging requires. The stdlib distro modules
  ship the universal ones — module-alpine pins `xz`, `zstd`, `util-linux`,
  `curl`, `kmod` to `alpine.main` (and module-debian/-ubuntu their
  equivalents) because module-core's monolithic source builds collide with
  the feeds' split library packaging: two packages owning the same
  `libblkid.so.1`/`libzstd.so.1`/`libkmod.so.2` path make apk/dpkg refuse
  to install. The rationale lives as comments next to each pin.
- **Project pins** in PROJECT.star's `prefer_modules`, merged on top —
  a project entry overrides a module default per unit, and pinning a name
  to `""` clears the default entirely, restoring plain module-priority
  resolution (i.e. the source-built unit).

Module defaults accumulate in module-priority order (a later module's pin
overrides an earlier module's for the same key). Every pin value is
validated against the known module names at load time, with
nearest-match suggestions on typos (the classic `"alpine"` →
`"alpine.main"` confusion).

Full resolution order for a (distro, name) pair, precomputed per distro at
load time:

1. `prefer_modules[distro][name]` pin, if set and non-empty — pin wins.
2. Highest-priority module whose unit is visible to the distro.

`osb desc <name>` prints the winning module and, whenever a pin or a
same-named unit in another module changes the answer per distro, a
`Resolves:` line for each.

## Feeds as synthetic modules

`module-alpine` and the apt distro modules expose entire upstream package
repositories as **feeds**, not per-package files. One `alpine_feed()` /
`apt_feed()` call registers a synthetic module backed by a checked-in
index (APKINDEX / Packages); every package in the index is available, and
a package's unit **materializes lazily** the first time a closure
references it. Naming a feed package in `deps` or an image's artifact
list is all it takes — no `.star` file is generated, and no
`prefer_modules` entry is needed unless a source unit also claims the
name (synthetic modules always lose name ties to real modules).

Refresh the checked-in indexes with `osb update-feeds` inside the module
repo.

## provides: virtual names

`provides = ["<virtual>"]` lets a unit satisfy a name other than its own.
The resolver keeps a Provides table; references to the virtual name
dereference to the provider. Two intended uses:

- **Leaf artifacts swapped per machine or project**: kernel (`linux`),
  `base-files`, init, bootloader. A machine's kernel config picks
  `linux-rpi4` for the Pi while images keep writing `"linux"`.
- **Toolchain dispatch (R9)**: the toolchain containers declare
  `provides = ["toolchain"]` plus a `distro` tag; classes depend on the
  virtual name `"toolchain"` and the resolver narrows to the one matching
  the consuming image's effective distro — alpine closures see
  toolchain-musl, debian closures see toolchain-debian.
- A third, narrower use: declaring ownership equivalence with a feed
  package that ships the same files (module-core's openssl declares
  `provides = ["libcrypto3", "libssl3"]` so Alpine's prebuilt packages
  aren't pulled in alongside it and apk doesn't see two owners of
  `libssl.so.3`).

### When NOT to use provides

Do **not** set `provides` on a build-time library, a generic tool (less,
htop, file, …), or a daemon that has a busybox alternative. Those should
ship side-by-side and be selected at boot from init scripts. Because the
provider is resolved per (machine, distro) context, misusing `provides`
forks every transitive consumer into a machine-specific package variant —
the exact fan-out the shared-unit model exists to avoid. If two packages
genuinely fight over a file path, the right tools are `replaces` (below)
or a `prefer_modules` pin, not a virtual name.

## replaces: sanctioned file shadowing

`replaces = ["busybox"]` on a unit tells apk to accept that this unit's
files may overwrite paths another package owns (util-linux's real `mount`
over busybox's applet). Without the annotation, image assembly fails on
the conflict — deliberately, since an undeclared overlap is usually a real
packaging bug. `replaces` resolves file conflicts at install time; it has
no effect on name resolution.
