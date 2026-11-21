//go:build !darwin

package managed

import (
	"errors"
	"syscall"
)

func bindInterface(syscall.RawConn, int) error { return errors.ErrUnsupported }
