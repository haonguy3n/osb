package device

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// dm-verity format constants. Block size and hash match the table osb writes
// into the signed cmdline; changing them would change every root hash.
const (
	verityBlockSize  = 4096
	verityDigestSize = 32 // sha256
	verityHashPerBlk = verityBlockSize / verityDigestSize
)

// VerityResult is the output of formatting a rootfs image for dm-verity: the
// hash-tree image to write into the rootfs-hash partition, plus the values that
// go into the kernel's dm-mod.create table (all hex-encoded for the cmdline).
type VerityResult struct {
	HashImage  []byte // the on-disk hash tree, laid out top level first
	RootHash   string // hex; the fingerprint anchored in the signed cmdline
	Salt       string // hex
	DataBlocks uint64 // number of 4096-byte data blocks in the rootfs
}

// FormatVerity builds a dm-verity v1 hash tree over data (a read-only rootfs
// image whose length must be a multiple of the 4096-byte block size). The salt
// is derived from the data so two builds of the same rootfs yield the same root
// hash, keeping images reproducible. The layout matches what the kernel's
// dm-verity target reconstructs from the dm-mod.create table: SHA-256 digests of
// salt||block, the tree built leaves-up, and stored on the hash device with the
// top (root) level first.
func FormatVerity(data []byte) (VerityResult, error) {
	if len(data) == 0 || len(data)%verityBlockSize != 0 {
		return VerityResult{}, fmt.Errorf("verity: data length %d is not a positive multiple of %d", len(data), verityBlockSize)
	}
	dataBlocks := uint64(len(data) / verityBlockSize)

	// Deterministic per-image salt: the digest of the whole rootfs. Salt binds
	// the tree to this image (defeats cross-image hash precomputation) while
	// staying a pure function of the content.
	saltArr := sha256.Sum256(data)
	salt := saltArr[:]

	// Leaf level: one digest per data block.
	level := hashBlocks(data, salt)

	// Build levels upward, collecting each padded level. The leaf level is
	// levels[0]; the single-block top level is levels[len-1].
	levels := [][]byte{level}
	for len(level) > verityBlockSize {
		level = hashBlocks(level, salt)
		levels = append(levels, level)
	}

	// Root hash is the digest of the single top-level block.
	top := levels[len(levels)-1]
	rootArr := sha256.Sum256(append(append([]byte{}, salt...), top...))

	// On-disk order is top level first, leaves last — the geometry the kernel
	// walks from hash_start.
	var img []byte
	for i := len(levels) - 1; i >= 0; i-- {
		img = append(img, levels[i]...)
	}

	return VerityResult{
		HashImage:  img,
		RootHash:   hex.EncodeToString(rootArr[:]),
		Salt:       hex.EncodeToString(salt),
		DataBlocks: dataBlocks,
	}, nil
}

// hashBlocks digests each 4096-byte block of in as SHA-256(salt||block) and
// returns the concatenated digests padded with zero blocks to a whole number of
// 4096-byte hash blocks (the padding the kernel expects at the tail of a level).
func hashBlocks(in, salt []byte) []byte {
	n := len(in) / verityBlockSize
	out := make([]byte, 0, n*verityDigestSize)
	h := sha256.New()
	for i := 0; i < n; i++ {
		block := in[i*verityBlockSize : (i+1)*verityBlockSize]
		h.Reset()
		h.Write(salt)
		h.Write(block)
		out = h.Sum(out)
	}
	// Pad up to a multiple of the block size.
	if rem := len(out) % verityBlockSize; rem != 0 {
		out = append(out, make([]byte, verityBlockSize-rem)...)
	}
	return out
}

// VerityCmdline renders the kernel command line that assembles the verified root
// from the signed UKI, given the format result and the GPT partition labels of
// the data and hash partitions. dm-mod.create builds /dev/dm-0 during early boot
// with no initramfs; root=/dev/dm-0 mounts it. Because this string lives inside
// the Secure-Boot-signed UKI, the embedded root hash is tamper-evident.
func VerityCmdline(base string, r VerityResult, dataLabel, hashLabel string) string {
	table := fmt.Sprintf("0 %d verity 1 PARTLABEL=%s PARTLABEL=%s %d %d %d 0 sha256 %s %s",
		r.DataBlocks*(verityBlockSize/512),
		dataLabel, hashLabel,
		verityBlockSize, verityBlockSize,
		r.DataBlocks,
		r.RootHash, r.Salt)
	return fmt.Sprintf(`dm-mod.create="dm-root,,,ro,%s" root=/dev/dm-0 ro rootwait %s`, table, base)
}
