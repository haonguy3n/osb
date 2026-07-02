# On-device upstream feeds

Dev images can opt into installing packages straight from the upstream
distro mirrors on the booted device — for experimentation, never as a
production update path. The mechanism is a **dormant companion unit**
named `upstream-feeds`, shipped per distro module and selected per image
distro (each variant carries a `distro` tag, so an alpine image gets the
alpine enabler, a debian image the debian one).

## How it stays dormant

Including `upstream-feeds` in an image (dev-image does; base-image does
not) installs only:

- `/usr/sbin/osb-enable-upstream-feeds` — the opt-in script
- `/usr/share/osb/upstream-keys/` — the upstream signing keys/keyring,
  **not** installed into the package manager's trust store

Nothing is configured or trusted until someone runs the script on the
device. Excluding the unit — the production default — leaves no script
and no keys behind.

## What enabling does

`osb-enable-upstream-feeds` wires the upstream mirror in a **held-back**
form, so a plain upgrade never silently pulls upstream packages over the
image's own:

- **Alpine**: appends the mirror to `/etc/apk/repositories` under the
  `@upstream` repository tag and trusts the upstream keys. Only an
  explicit `apk add <pkg>@upstream` reaches it; `apk add`/`apk upgrade`
  without the tag never do.
- **Debian/Ubuntu**: writes `/etc/apt/sources.list.d/osb-upstream.list`
  with a per-source `signed-by` key (no global trust anchor) and pins it
  to priority 100 in `/etc/apt/preferences.d/osb-upstream.pref`. A plain
  `apt upgrade` never reaches it; an explicit
  `apt install -t <suite> <pkg>` does.

## Invariants

- The mirror, release/suite, and sections baked into each enabler script
  must stay in sync with the `alpine_feed(...)` / `apt_feed(...)` calls in
  that module's MODULE.star — the on-device feed must match the ABI the
  image was built against.
- The osb-built base system always wins by default; upstream packages are
  reachable only through the explicit tag/suite syntax above.
