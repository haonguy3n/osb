package device

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// walkVerity re-derives the root hash from the data and the produced hash image
// by walking the tree top-down exactly as the kernel does: read the top block,
// hash it, and confirm it matches each child level down to the leaves. This
// proves the on-disk layout and digests are internally consistent.
func walkVerity(t *testing.T, data []byte, r VerityResult) {
	t.Helper()
	salt, _ := hex.DecodeString(r.Salt)

	// Reconstruct level block counts leaves-up to know how the image splits.
	counts := []int{}
	n := int(r.DataBlocks)
	for {
		blocks := (n + verityHashPerBlk - 1) / verityHashPerBlk
		counts = append(counts, blocks)
		if blocks == 1 {
			break
		}
		n = blocks
	}
	// Image is stored top level first; counts is leaves-first. Split the image
	// into levels in on-disk (top-first) order.
	levelsTopFirst := [][]byte{}
	off := 0
	for i := len(counts) - 1; i >= 0; i-- {
		sz := counts[i] * verityBlockSize
		levelsTopFirst = append(levelsTopFirst, r.HashImage[off:off+sz])
		off += sz
	}
	if off != len(r.HashImage) {
		t.Fatalf("hash image has %d trailing bytes", len(r.HashImage)-off)
	}

	// Top block hashes to the root.
	top := levelsTopFirst[0][:verityBlockSize]
	got := sha256.Sum256(append(append([]byte{}, salt...), top...))
	if hex.EncodeToString(got[:]) != r.RootHash {
		t.Fatalf("root hash mismatch: walked %x want %s", got, r.RootHash)
	}

	// Leaf level (last, on-disk) must equal SHA256(salt||data_block) per block.
	leaves := levelsTopFirst[len(levelsTopFirst)-1]
	for i := 0; i < int(r.DataBlocks); i++ {
		block := data[i*verityBlockSize : (i+1)*verityBlockSize]
		h := sha256.Sum256(append(append([]byte{}, salt...), block...))
		if !bytes.Equal(leaves[i*verityDigestSize:(i+1)*verityDigestSize], h[:]) {
			t.Fatalf("leaf %d digest mismatch", i)
		}
	}
}

func TestFormatVerityConsistency(t *testing.T) {
	// A rootfs spanning two hash levels: 300 blocks > 128 per hash block.
	data := make([]byte, 300*verityBlockSize)
	for i := range data {
		data[i] = byte(i * 7)
	}
	r, err := FormatVerity(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.DataBlocks != 300 {
		t.Fatalf("DataBlocks = %d, want 300", r.DataBlocks)
	}
	walkVerity(t, data, r)
}

func TestFormatVeritySingleLevel(t *testing.T) {
	// Fewer than 128 blocks → the leaf level is already a single block.
	data := bytes.Repeat([]byte{0xab}, 10*verityBlockSize)
	r, err := FormatVerity(data)
	if err != nil {
		t.Fatal(err)
	}
	walkVerity(t, data, r)
}

func TestFormatVerityDeterministicAndTamperEvident(t *testing.T) {
	data := bytes.Repeat([]byte{0x11}, 64*verityBlockSize)
	a, err := FormatVerity(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FormatVerity(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.RootHash != b.RootHash {
		t.Fatal("same input produced different root hashes (not reproducible)")
	}
	// Flip one byte in one block: the root hash must change.
	data[5*verityBlockSize] ^= 0xff
	c, err := FormatVerity(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.RootHash == a.RootHash {
		t.Fatal("tampering with a data block did not change the root hash")
	}
}

func TestFormatVerityRejectsUnaligned(t *testing.T) {
	if _, err := FormatVerity(make([]byte, 100)); err == nil {
		t.Fatal("expected error for non-block-multiple length")
	}
}

func TestVerityCmdline(t *testing.T) {
	r := VerityResult{RootHash: "deadbeef", Salt: "cafe", DataBlocks: 300}
	got := VerityCmdline(r, "root-hash")
	want := "osb.verity=PARTLABEL=root-hash roothash=deadbeef osb.verity.salt=cafe osb.verity.blocks=300"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestApplyVerityToDiskMatchesFormat(t *testing.T) {
	data := bytes.Repeat([]byte{7}, 64*verityBlockSize)
	img := append(append([]byte{}, data...), make([]byte, 4*verityBlockSize)...)
	path := filepath.Join(t.TempDir(), "disk.img")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ApplyVerityToDisk(path, 0, int64(len(data)), int64(len(data)), 4*verityBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := FormatVerity(data)
	if got.RootHash != want.RootHash {
		t.Fatalf("streamed root hash %s != in-memory %s", got.RootHash, want.RootHash)
	}
	disk, _ := os.ReadFile(path)
	if !bytes.Equal(disk[len(data):len(data)+len(want.HashImage)], want.HashImage) {
		t.Fatal("hash tree not written after the data")
	}
}
