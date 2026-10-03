package device

import "errors"

var ErrPermission = errors.New("permission denied")

var ErrBusy = errors.New("device busy")
