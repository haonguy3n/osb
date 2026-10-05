#!/bin/sh
failed=0

check() {
    name=$1
    shift
    if out=$("$@" 2>&1); then
        echo "ok   $name"
    else
        echo "FAIL $name"
        [ -n "$out" ] && echo "$out" | sed 's/^/     /'
        failed=1
    fi
}

root_rw() {
    awk '$2 == "/" { print $4 }' /proc/mounts | grep '^rw'
}

root_grew() {
    df -Pk / | awk 'NR == 2 { print $2 " KiB"; exit !($2 > 4000000) }'
}

hostname_set() {
    [ "$(hostname)" = "$(cat /etc/hostname)" ]
}

default_route() {
    awk '$2 == "00000000" { found = 1 } END { exit !found }' /proc/net/route
}

installed() {
    if command -v dpkg >/dev/null; then
        dpkg -s "$@" >/dev/null
    else
        apk info -e "$@"
    fi
}

hello() {
    hello-cpp | grep 'hello, world'
}

no_failed_services() {
    if command -v systemctl >/dev/null; then
        systemctl is-system-running --wait >/dev/null
        failed_units=$(systemctl --failed --no-legend --plain)
    else
        failed_units=$(rc-status --crashed)
    fi
    echo "$failed_units"
    [ -z "$failed_units" ]
}

echo "$(uname -srm) on $(hostname)"
check "root is mounted read-write" root_rw
check "root grew to fill the disk" root_grew
check "hostname matches /etc/hostname" hostname_set
check "default route" default_route
check "packages installed" installed hello-cpp libgreet
check "hello-cpp runs" hello
check "no failed services" no_failed_services

exit $failed
