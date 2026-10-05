//go:build linux

package device

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	blockSize = 512

	bufSize = 4 * 1024 * 1024

	progressByteThreshold = 16 * 1024 * 1024
	progressTimeThreshold = 250 * time.Millisecond
)

func Write(imagePath, devicePath string, progress func(written, total int64)) error {
	src, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("open image: %w", err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat image: %w", err)
	}
	total := info.Size()

	dst, err := os.OpenFile(devicePath, os.O_WRONLY|syscall.O_EXCL|syscall.O_DIRECT, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return ErrPermission
		}
		if errors.Is(err, syscall.EBUSY) {
			return ErrBusy
		}
		return fmt.Errorf("open device: %w", err)
	}
	defer dst.Close()

	buf, err := unix.Mmap(-1, 0, bufSize,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return fmt.Errorf("alloc aligned buffer: %w", err)
	}
	defer unix.Munmap(buf)

	if err := copyAlignedWithProgress(dst, src, buf, total, progress); err != nil {
		return err
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

func copyAlignedWithProgress(dst io.Writer, src io.Reader, buf []byte, total int64, progress func(written, total int64)) error {
	var written int64
	lastBytes := int64(0)
	lastTime := time.Now()

	for {
		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			padded := alignUp(n, blockSize)
			for i := n; i < padded; i++ {
				buf[i] = 0
			}
			if _, werr := dst.Write(buf[:padded]); werr != nil {
				return fmt.Errorf("write: %w", werr)
			}
			written += int64(n)

			now := time.Now()
			if progress != nil &&
				(written-lastBytes >= progressByteThreshold ||
					now.Sub(lastTime) >= progressTimeThreshold) {
				progress(written, total)
				lastBytes = written
				lastTime = now
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("read: %w", rerr)
		}
	}

	if progress != nil {
		progress(written, total)
	}
	return nil
}

func alignUp(n, align int) int {
	return ((n + align - 1) / align) * align
}
