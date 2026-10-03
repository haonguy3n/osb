//go:build !linux

package device

import (
	"fmt"
	"runtime"
)

func Write(imagePath, devicePath string, progress func(written, total int64)) error {
	return fmt.Errorf("flash write not supported on %s", runtime.GOOS)
}
