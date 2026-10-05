FROM alpine:3.21

RUN apk add --no-cache \
    bubblewrap \
    apk-tools \
    git \
    tar \
    gzip \
    bzip2 \
    xz \
    patch \
    coreutils \
    bash \
    make \
    gcc \
    g++ \
    musl-dev \
    linux-headers \
    sfdisk \
    e2fsprogs \
    dosfstools \
    mtools \
    xorriso \
    losetup \
    mount \
    autoconf \
    automake \
    libtool \
    perl \
    bc \
    flex \
    bison \
    elfutils-dev \
    openssl-dev \
    openssl \
    diffutils \
    findutils \
    sed \
    cmake

RUN mkdir -p /build/src /build/destdir

RUN for i in $(seq 0 7); do mknod -m 0660 /dev/loop$i b 7 $i 2>/dev/null || true; done

WORKDIR /project
