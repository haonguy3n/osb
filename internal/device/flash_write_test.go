//go:build linux

package device

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestCopyAlignedWritesAllAndReportsProgress(t *testing.T) {
	const size = 8 * 1024 * 1024
	src := make([]byte, size)
	if _, err := rand.Read(src); err != nil {
		t.Fatal(err)
	}

	var dst bytes.Buffer
	buf := make([]byte, bufSize)
	var calls int
	var lastWritten int64

	err := copyAlignedWithProgress(&dst, bytes.NewReader(src), buf, int64(size), func(written, total int64) {
		calls++
		lastWritten = written
		if total != int64(size) {
			t.Errorf("progress total = %d, want %d", total, size)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(dst.Bytes(), src) {
		t.Error("destination contents do not match source")
	}
	if dst.Len() != size {
		t.Errorf("destination length = %d, want %d (no padding for aligned input)", dst.Len(), size)
	}
	if calls < 1 {
		t.Errorf("expected at least one progress call, got %d", calls)
	}
	if lastWritten != int64(size) {
		t.Errorf("final progress written = %d, want %d", lastWritten, size)
	}
}

func TestCopyAlignedZeroPadsTrailingPartialBlock(t *testing.T) {
	const size = 4*1024*1024 + 17
	src := make([]byte, size)
	if _, err := rand.Read(src); err != nil {
		t.Fatal(err)
	}

	var dst bytes.Buffer
	buf := make([]byte, bufSize)
	var lastWritten int64

	err := copyAlignedWithProgress(&dst, bytes.NewReader(src), buf, int64(size), func(written, total int64) {
		lastWritten = written
	})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(dst.Bytes()[:size], src) {
		t.Error("first source-size bytes don't match source")
	}

	expectedLen := alignUp(size, blockSize)
	if dst.Len() != expectedLen {
		t.Errorf("destination length = %d, want %d (aligned to blockSize)", dst.Len(), expectedLen)
	}

	for i := size; i < expectedLen; i++ {
		if dst.Bytes()[i] != 0 {
			t.Errorf("padding byte at offset %d = %d, want 0", i, dst.Bytes()[i])
			break
		}
	}

	if lastWritten != int64(size) {
		t.Errorf("final progress written = %d, want %d (real, not padded)", lastWritten, size)
	}
}

func TestCopyAlignedEmptySource(t *testing.T) {
	var dst bytes.Buffer
	buf := make([]byte, bufSize)
	var lastWritten int64
	err := copyAlignedWithProgress(&dst, bytes.NewReader(nil), buf, 0, func(written, total int64) {
		lastWritten = written
	})
	if err != nil {
		t.Fatal(err)
	}
	if dst.Len() != 0 {
		t.Errorf("destination length = %d, want 0", dst.Len())
	}
	if lastWritten != 0 {
		t.Errorf("final progress written = %d, want 0", lastWritten)
	}
}

func TestAlignUp(t *testing.T) {
	cases := []struct {
		n, align, want int
	}{
		{0, 512, 0},
		{1, 512, 512},
		{511, 512, 512},
		{512, 512, 512},
		{513, 512, 1024},
		{17, 512, 512},
		{4*1024*1024 + 17, 512, 4*1024*1024 + 512},
	}
	for _, c := range cases {
		got := alignUp(c.n, c.align)
		if got != c.want {
			t.Errorf("alignUp(%d, %d) = %d, want %d", c.n, c.align, got, c.want)
		}
	}
}
