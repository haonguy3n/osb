# Testing

Two layers: the Go test suite (fast, no external dependencies, what hosted
CI runs) and the full-matrix suites in `test-suites.yaml` (image builds,
QEMU boot tests, SDK verification — needs a host with Docker, and KVM for
the boot suites).

## Quick

```sh
make test          # go test ./...
```

## Full matrix

```sh
make test-full                      # every suite in test-suites.yaml
go run ./cmd/testsuite -list        # see what exists
go run ./cmd/testsuite lint unit    # run specific suites
```

The runner builds a fresh `osb` binary once and exposes it to steps as
`$OSB`. Suites with `dir: scratch` run inside a throwaway project
(created with `osb init` at the `scratch:` path in the yaml, default
`/tmp/osb-testsuite-project`) so nothing touches your real projects.

### Requirements and skipping

Each suite declares `requires:`; unmet requirements skip the suite with a
notice rather than failing, so the same invocation works on a laptop
without KVM and on a full build host. Probes:

| requirement    | probe                                       | needed by            |
| -------------- | ------------------------------------------- | -------------------- |
| `docker`       | `docker info` succeeds                      | any image/unit build |
| `kvm`          | `/dev/kvm` exists                           | boot tests           |
| `binfmt-arm64` | `/proc/sys/fs/binfmt_misc/qemu-aarch64`     | arm64 builds/boots   |
| `network`      | ping to the Alpine CDN                      | feed/source fetches  |

Register arm64 binfmt with `osb container binfmt` if the arm64 suites
skip.

### The suites

- **lint / unit / machines** — what hosted CI runs (`.github/workflows/ci.yml`).
- **units-build** — builds `efitools` from source, exercising a real
  dependency chain (gnu-efi → openssl → zlib) through the toolchain
  container and patch application.
- **image-alpine** — assembles base-image and dev-image (apk rootfs +
  disk image paths).
- **boot-alpine / boot-secureboot / boot-ab / boot-debian / boot-ubuntu /
  boot-arm64** — `osb build` + `osb run -boot-test` per machine: BIOS,
  UEFI+GRUB, Secure Boot (signed UKI), Secure Boot + dm-verity,
  Secure Boot + A/B, plain A/B, the apt distros, and arm64. The boot test
  waits for a login prompt, SSHes in for a health check, and powers off;
  any failure is non-zero.
- **sdk** — generates the SDK for base-image and compiles + runs a
  program inside it.

Suites run in declaration order and continue past failures; the runner
prints a summary table and exits non-zero if anything failed.

### CI

Hosted runners (`.github/workflows/ci.yml`) run lint/unit/machines only —
they lack Docker-in-Docker reliability and KVM. The boot-test job runs on
a self-hosted runner when the `OSB_SELFHOSTED_RUNNER` repo variable is
set; pointing that runner's job at `go run ./cmd/testsuite` keeps CI and
local runs on the same definition.
