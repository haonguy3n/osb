# Read-only root with a tmpfs overlay (feature: readonly).
# Expects tests/features/lib.sh to be prepended (see the header there).
echo "$(uname -srm) on $(hostname)"
check "kernel command line has osb.overlay=tmpfs" cmdline_has osb.overlay=tmpfs
check "root is an overlay" root_is_overlay
check "overlay upper is tmpfs" upper_root_is_tmpfs
check "overlay lower is read-only" lower_root_is_readonly
check "writes land in the overlay" root_writable
check "no failed services" no_failed_services
finish
