package device

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const (
	verityBlockSize  = 4096
	verityDigestSize = 32
	verityHashPerBlk = verityBlockSize / verityDigestSize
)

type VerityResult struct {
	HashImage  []byte
	RootHash   string
	Salt       string
	DataBlocks uint64
}

func FormatVerity(data []byte) (VerityResult, error) {
	return formatVerity(bytes.NewReader(data), int64(len(data)))
}

func formatVerity(r io.ReaderAt, size int64) (VerityResult, error) {
	if size <= 0 || size%verityBlockSize != 0 {
		return VerityResult{}, fmt.Errorf("verity: data length %d is not a positive multiple of %d", size, verityBlockSize)
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(r, 0, size)); err != nil {
		return VerityResult{}, err
	}
	salt := sum.Sum(nil)

	level, err := hashStream(io.NewSectionReader(r, 0, size), salt)
	if err != nil {
		return VerityResult{}, err
	}
	levels := [][]byte{level}
	for len(level) > verityBlockSize {
		if level, err = hashStream(bytes.NewReader(level), salt); err != nil {
			return VerityResult{}, err
		}
		levels = append(levels, level)
	}
	root := sha256.Sum256(append(append([]byte{}, salt...), levels[len(levels)-1]...))
	var img []byte
	for i := len(levels) - 1; i >= 0; i-- {
		img = append(img, levels[i]...)
	}
	return VerityResult{
		HashImage:  img,
		RootHash:   hex.EncodeToString(root[:]),
		Salt:       hex.EncodeToString(salt),
		DataBlocks: uint64(size / verityBlockSize),
	}, nil
}

func hashStream(r io.Reader, salt []byte) ([]byte, error) {
	var out []byte
	block := make([]byte, verityBlockSize)
	h := sha256.New()
	for {
		if _, err := io.ReadFull(r, block); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		h.Reset()
		h.Write(salt)
		h.Write(block)
		out = h.Sum(out)
	}
	if rem := len(out) % verityBlockSize; rem != 0 {
		out = append(out, make([]byte, verityBlockSize-rem)...)
	}
	return out, nil
}

func ApplyVerityToDisk(diskPath string, dataOff, dataLen, hashOff, hashLen int64) (VerityResult, error) {
	f, err := os.OpenFile(diskPath, os.O_RDWR, 0)
	if err != nil {
		return VerityResult{}, err
	}
	defer f.Close()
	res, err := formatVerity(io.NewSectionReader(f, dataOff, dataLen), dataLen)
	if err != nil {
		return VerityResult{}, err
	}
	if int64(len(res.HashImage)) > hashLen {
		return VerityResult{}, fmt.Errorf("verity: hash tree is %d bytes but the hash partition is only %d", len(res.HashImage), hashLen)
	}
	if _, err := f.WriteAt(res.HashImage, hashOff); err != nil {
		return VerityResult{}, fmt.Errorf("verity: writing hash tree: %w", err)
	}
	return res, f.Sync()
}

func VerityCmdline(r VerityResult, hashPart string) string {
	return fmt.Sprintf("osb.verity=PARTLABEL=%s roothash=%s osb.verity.salt=%s osb.verity.blocks=%d",
		hashPart, r.RootHash, r.Salt, r.DataBlocks)
}
