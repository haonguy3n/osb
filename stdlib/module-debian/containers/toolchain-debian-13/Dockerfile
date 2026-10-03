FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install --no-install-recommends -y \
        ca-certificates \
        bubblewrap \
        git \
        tar \
        gzip \
        bzip2 \
        xz-utils \
        patch \
        coreutils \
        bash \
        make \
        gcc \
        g++ \
        libc6-dev \
        linux-libc-dev \
        autoconf \
        automake \
        libtool \
        pkgconf \
        perl \
        bc \
        flex \
        bison \
        sed \
        gawk \
        cmake \
        dpkg-dev \
        apt-utils \
        debootstrap \
        mmdebstrap \
        gpg \
        gnupg \
        fakeroot \
        eatmydata \
        e2fsprogs \
        dosfstools \
        mtools \
        xorriso \
        fdisk \
        mount \
        diffutils \
        findutils && \
    rm -rf /var/lib/apt/lists/*

RUN mkdir -p /build/src /build/destdir

RUN for i in $(seq 0 7); do mknod -m 0660 /dev/loop$i b 7 $i 2>/dev/null || true; done

WORKDIR /project
